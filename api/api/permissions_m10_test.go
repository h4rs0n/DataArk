package api

import (
	"DataArk/auth"
	"DataArk/discovery"
	"DataArk/recommendation"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDiscoverySourceManagementRequiresOwner(t *testing.T) {
	oldCreate := createDiscoverySource
	t.Cleanup(func() { createDiscoverySource = oldCreate })
	called := 0
	createDiscoverySource = func(name string, rawURL string, sourceType string, enabled bool) (*discovery.DiscoverySource, error) {
		called++
		return &discovery.DiscoverySource{ID: 7, Name: name, URL: rawURL, Type: sourceType, Enabled: enabled}, nil
	}
	body := []byte(`{"name":"Feed","url":"https://example.com/feed.xml","type":"feed","enabled":true}`)

	memberResponse := performUserControllerRequest(http.MethodPost, "/discovery/sources", body, &auth.User{ID: 2, Username: "member", Role: auth.UserRoleMember}, CreateDiscoverySource)
	if memberResponse.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", memberResponse.Code)
	}
	if called != 0 {
		t.Fatalf("source creation called %d times for member", called)
	}

	ownerResponse := performUserControllerRequest(http.MethodPost, "/discovery/sources", body, &auth.User{ID: 1, Username: "admin", Role: auth.UserRoleOwner}, CreateDiscoverySource)
	if ownerResponse.Code != http.StatusCreated {
		t.Fatalf("owner status = %d, want 201: %s", ownerResponse.Code, ownerResponse.Body.String())
	}
	if called != 1 {
		t.Fatalf("source creation called %d times, want 1", called)
	}
}

func TestRecommendationRetryRequiresOwner(t *testing.T) {
	oldRegenerate := regenerateRecommendations
	t.Cleanup(func() { regenerateRecommendations = oldRegenerate })
	called := 0
	regenerateRecommendations = func(context.Context, uint, string) (*recommendationSnapshotAlias, error) {
		called++
		return nil, nil
	}

	memberResponse := performUserControllerRequest(http.MethodPost, "/admin/recommendations/generate", nil, &auth.User{ID: 2, Username: "member", Role: auth.UserRoleMember}, GenerateRecommendationDay)
	if memberResponse.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", memberResponse.Code)
	}
	if called != 0 {
		t.Fatalf("regeneration called %d times for member", called)
	}
}

func TestRecommendationSupplementRequiresOwner(t *testing.T) {
	oldSupplement := supplementRecommendations
	t.Cleanup(func() { supplementRecommendations = oldSupplement })
	called := 0
	supplementRecommendations = func(context.Context, uint, string) (*recommendationSnapshotAlias, error) {
		called++
		return nil, nil
	}
	response := performUserControllerRequest(http.MethodPost, "/admin/recommendations/supplement", nil, &auth.User{ID: 2, Role: auth.UserRoleMember}, SupplementRecommendationDay)
	if response.Code != http.StatusForbidden || called != 0 {
		t.Fatalf("member supplement status=%d calls=%d", response.Code, called)
	}
}

func TestSiteStatusAndManualBackfillRequireOwner(t *testing.T) {
	oldUpdate := updateDiscoverySiteStatus
	oldBackfill := requestDiscoverySiteBackfill
	oldSitemapBackfill := requestDiscoverySiteSitemapBackfill
	t.Cleanup(func() {
		updateDiscoverySiteStatus = oldUpdate
		requestDiscoverySiteBackfill = oldBackfill
		requestDiscoverySiteSitemapBackfill = oldSitemapBackfill
	})
	statusCalls := 0
	backfillCalls := 0
	sitemapBackfillCalls := 0
	updateDiscoverySiteStatus = func(siteID uint, status string, reason string) (*discovery.DiscoverySite, error) {
		statusCalls++
		return &discovery.DiscoverySite{ID: siteID, Status: status, OperationalDetails: reason}, nil
	}
	requestDiscoverySiteBackfill = func(context.Context, uint) error {
		backfillCalls++
		return nil
	}
	requestDiscoverySiteSitemapBackfill = func(_ context.Context, siteID uint, rawURL string) error {
		if siteID != 9 || rawURL != "https://example.com/sitemap.xml" {
			t.Fatalf("sitemap request = %d %q", siteID, rawURL)
		}
		sitemapBackfillCalls++
		return nil
	}
	member := &auth.User{ID: 2, Username: "member", Role: auth.UserRoleMember}
	owner := &auth.User{ID: 1, Username: "admin", Role: auth.UserRoleOwner}
	body := []byte(`{"status":"paused","reason":"maintenance"}`)

	memberStatus := performUserPathControllerRequestWithBody(http.MethodPut, "/discovery/sites/:id/status", "/discovery/sites/9/status", body, member, UpdateDiscoverySiteStatus)
	memberBackfill := performUserPathControllerRequest(http.MethodPost, "/discovery/sites/:id/backfill", "/discovery/sites/9/backfill", member, RequestDiscoverySiteBackfill)
	memberSitemap := performUserPathControllerRequestWithBody(http.MethodPost, "/discovery/sites/:id/sitemap-backfill", "/discovery/sites/9/sitemap-backfill", []byte(`{"url":"https://example.com/sitemap.xml"}`), member, RequestDiscoverySiteSitemapBackfill)
	if memberStatus.Code != http.StatusForbidden || memberBackfill.Code != http.StatusForbidden || memberSitemap.Code != http.StatusForbidden || statusCalls != 0 || backfillCalls != 0 || sitemapBackfillCalls != 0 {
		t.Fatalf("member results status=%d backfill=%d sitemap=%d calls=%d/%d/%d", memberStatus.Code, memberBackfill.Code, memberSitemap.Code, statusCalls, backfillCalls, sitemapBackfillCalls)
	}

	ownerStatus := performUserPathControllerRequestWithBody(http.MethodPut, "/discovery/sites/:id/status", "/discovery/sites/9/status", body, owner, UpdateDiscoverySiteStatus)
	ownerBackfill := performUserPathControllerRequest(http.MethodPost, "/discovery/sites/:id/backfill", "/discovery/sites/9/backfill", owner, RequestDiscoverySiteBackfill)
	ownerSitemap := performUserPathControllerRequestWithBody(http.MethodPost, "/discovery/sites/:id/sitemap-backfill", "/discovery/sites/9/sitemap-backfill", []byte(`{"url":"https://example.com/sitemap.xml"}`), owner, RequestDiscoverySiteSitemapBackfill)
	if ownerStatus.Code != http.StatusOK || ownerBackfill.Code != http.StatusAccepted || ownerSitemap.Code != http.StatusAccepted || statusCalls != 1 || backfillCalls != 1 || sitemapBackfillCalls != 1 {
		t.Fatalf("owner results status=%d backfill=%d sitemap=%d calls=%d/%d/%d", ownerStatus.Code, ownerBackfill.Code, ownerSitemap.Code, statusCalls, backfillCalls, sitemapBackfillCalls)
	}
}

func TestCandidateInteractionUsesAuthenticatedUserIdentity(t *testing.T) {
	oldMarkRead := markCandidateRead
	t.Cleanup(func() { markCandidateRead = oldMarkRead })
	var gotUserID uint
	var gotCandidateID uint
	markCandidateRead = func(userID uint, candidateID uint) (*discovery.UserDiscoveryCandidate, error) {
		gotUserID = userID
		gotCandidateID = candidateID
		return &discovery.UserDiscoveryCandidate{DiscoveryCandidate: discovery.DiscoveryCandidate{ID: candidateID}}, nil
	}

	response := performUserPathControllerRequest(http.MethodPost, "/discovery/candidates/:id/read", "/discovery/candidates/44/read", &auth.User{ID: 202, Username: "reader", Role: auth.UserRoleMember}, MarkDiscoveryCandidateRead)
	if response.Code != http.StatusOK {
		t.Fatalf("read status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if gotUserID != 202 || gotCandidateID != 44 {
		t.Fatalf("interaction identity user=%d candidate=%d", gotUserID, gotCandidateID)
	}
}

func TestOperationsRequireOwnerAndInventoryUsesCurrentUser(t *testing.T) {
	oldOperations := getDiscoverySiteOperations
	oldInventory := getCandidateInventory
	t.Cleanup(func() {
		getDiscoverySiteOperations = oldOperations
		getCandidateInventory = oldInventory
	})
	operationsCalls := 0
	getDiscoverySiteOperations = func(siteID uint) (*discovery.DiscoverySiteOperations, error) {
		operationsCalls++
		return &discovery.DiscoverySiteOperations{Site: discovery.DiscoverySite{ID: siteID}}, nil
	}
	var inventoryUserID uint
	getCandidateInventory = func(userID uint) (*recommendation.CandidateInventory, error) {
		inventoryUserID = userID
		return &recommendation.CandidateInventory{UserID: userID, UserAvailableCandidates: 12}, nil
	}
	member := &auth.User{ID: 202, Username: "member", Role: auth.UserRoleMember}
	owner := &auth.User{ID: 1, Username: "admin", Role: auth.UserRoleOwner}

	memberOperations := performUserPathControllerRequest(http.MethodGet, "/discovery/sites/:id/operations", "/discovery/sites/9/operations", member, GetDiscoverySiteOperations)
	if memberOperations.Code != http.StatusForbidden || operationsCalls != 0 {
		t.Fatalf("member operations status=%d calls=%d", memberOperations.Code, operationsCalls)
	}
	ownerOperations := performUserPathControllerRequest(http.MethodGet, "/discovery/sites/:id/operations", "/discovery/sites/9/operations", owner, GetDiscoverySiteOperations)
	if ownerOperations.Code != http.StatusOK || operationsCalls != 1 {
		t.Fatalf("owner operations status=%d calls=%d", ownerOperations.Code, operationsCalls)
	}
	memberInventory := performUserPathControllerRequest(http.MethodGet, "/recommendations/inventory", "/recommendations/inventory", member, GetRecommendationInventory)
	if memberInventory.Code != http.StatusOK || inventoryUserID != member.ID {
		t.Fatalf("member inventory status=%d user=%d", memberInventory.Code, inventoryUserID)
	}
}

// Keep the concrete return type coupled to the production function while making
// the permission test independent from recommendation internals.
type recommendationSnapshotAlias = recommendation.RecommendationDaySnapshot

func performUserControllerRequest(method string, target string, body []byte, user *auth.User, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return performUserPathControllerRequestWithBody(method, target, target, body, user, handler)
}

func performUserPathControllerRequest(method string, routePath string, target string, user *auth.User, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return performUserPathControllerRequestWithBody(method, routePath, target, nil, user, handler)
}

func performUserPathControllerRequestWithBody(method string, routePath string, target string, body []byte, user *auth.User, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, func(c *gin.Context) {
		c.Set("user", user)
		c.Set("user_id", user.ID)
		c.Next()
	}, handler)
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
