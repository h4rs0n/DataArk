package api

import (
	"DataArk/auth"
	"DataArk/recommendation"
	"context"
	"net/http"
	"testing"
)

func TestDiscoveryFeedEndpointsUseAuthenticatedUser(t *testing.T) {
	oldGet, oldRefresh := getCurrentDiscoveryFeed, refreshDiscoveryFeed
	t.Cleanup(func() {
		getCurrentDiscoveryFeed, refreshDiscoveryFeed = oldGet, oldRefresh
	})
	getCurrentDiscoveryFeed = func(userID uint) (*recommendation.RecommendationFeedSnapshot, error) {
		if userID != 4101 {
			t.Fatalf("GET user = %d", userID)
		}
		return &recommendation.RecommendationFeedSnapshot{Items: []recommendation.RecommendationItem{}}, nil
	}
	refreshDiscoveryFeed = func(_ context.Context, userID uint, limit int) (*recommendation.RecommendationFeedSnapshot, error) {
		if userID != 4101 || limit != 10 {
			t.Fatalf("POST identity = %d limit=%d", userID, limit)
		}
		return &recommendation.RecommendationFeedSnapshot{
			Batch: &recommendation.RecommendationFeedBatch{ID: 8, UserID: userID, RequestedCount: 10, ActualCount: 0},
			Items: []recommendation.RecommendationItem{},
		}, nil
	}
	user := &auth.User{ID: 4101, Role: auth.UserRoleMember}
	getResponse := performUserControllerRequest(http.MethodGet, "/recommendations/discovery-feed", nil, user, GetDiscoveryRecommendationFeed)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	postResponse := performUserControllerRequest(http.MethodPost, "/recommendations/discovery-feed/refresh", nil, user, RefreshDiscoveryRecommendationFeed)
	if postResponse.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", postResponse.Code, postResponse.Body.String())
	}
}
