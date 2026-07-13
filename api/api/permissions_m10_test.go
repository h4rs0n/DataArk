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

func TestDestructiveRecommendationGenerationRequiresOwner(t *testing.T) {
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

func TestSiteStatusAndManualBackfillRequireOwner(t *testing.T) {
	oldUpdate := updateDiscoverySiteStatus
	oldBackfill := requestDiscoverySiteBackfill
	t.Cleanup(func() {
		updateDiscoverySiteStatus = oldUpdate
		requestDiscoverySiteBackfill = oldBackfill
	})
	statusCalls := 0
	backfillCalls := 0
	updateDiscoverySiteStatus = func(siteID uint, status string, reason string) (*discovery.DiscoverySite, error) {
		statusCalls++
		return &discovery.DiscoverySite{ID: siteID, Status: status, OperationalDetails: reason}, nil
	}
	requestDiscoverySiteBackfill = func(context.Context, uint) error {
		backfillCalls++
		return nil
	}
	member := &auth.User{ID: 2, Username: "member", Role: auth.UserRoleMember}
	owner := &auth.User{ID: 1, Username: "admin", Role: auth.UserRoleOwner}
	body := []byte(`{"status":"paused","reason":"maintenance"}`)

	memberStatus := performUserPathControllerRequestWithBody(http.MethodPut, "/discovery/sites/:id/status", "/discovery/sites/9/status", body, member, UpdateDiscoverySiteStatus)
	memberBackfill := performUserPathControllerRequest(http.MethodPost, "/discovery/sites/:id/backfill", "/discovery/sites/9/backfill", member, RequestDiscoverySiteBackfill)
	if memberStatus.Code != http.StatusForbidden || memberBackfill.Code != http.StatusForbidden || statusCalls != 0 || backfillCalls != 0 {
		t.Fatalf("member results status=%d backfill=%d calls=%d/%d", memberStatus.Code, memberBackfill.Code, statusCalls, backfillCalls)
	}

	ownerStatus := performUserPathControllerRequestWithBody(http.MethodPut, "/discovery/sites/:id/status", "/discovery/sites/9/status", body, owner, UpdateDiscoverySiteStatus)
	ownerBackfill := performUserPathControllerRequest(http.MethodPost, "/discovery/sites/:id/backfill", "/discovery/sites/9/backfill", owner, RequestDiscoverySiteBackfill)
	if ownerStatus.Code != http.StatusOK || ownerBackfill.Code != http.StatusAccepted || statusCalls != 1 || backfillCalls != 1 {
		t.Fatalf("owner results status=%d backfill=%d calls=%d/%d", ownerStatus.Code, ownerBackfill.Code, statusCalls, backfillCalls)
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
