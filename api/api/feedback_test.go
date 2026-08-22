package api

import (
	"DataArk/auth"
	"DataArk/recommendation"
	"net/http"
	"testing"
)

func TestFeedbackM13APIShowsCurrentHistoryAndReset(t *testing.T) {
	oldCurrent := getCurrentRecommendationFeedback
	oldHistory := listRecommendationFeedbackHistory
	oldReset := resetUserRecommendationPreferences
	t.Cleanup(func() {
		getCurrentRecommendationFeedback = oldCurrent
		listRecommendationFeedbackHistory = oldHistory
		resetUserRecommendationPreferences = oldReset
	})
	getCurrentRecommendationFeedback = func(userID uint, itemID uint) (*recommendation.RecommendationFeedback, error) {
		if userID != 91 || itemID != 12 {
			t.Fatalf("identity = %d/%d", userID, itemID)
		}
		return &recommendation.RecommendationFeedback{ID: 3, UserID: userID, RecommendationItemID: itemID, Action: recommendation.RecommendationFeedbackValuable, IsCurrent: true}, nil
	}
	listRecommendationFeedbackHistory = func(userID uint, itemID uint) ([]recommendation.RecommendationFeedback, error) {
		return []recommendation.RecommendationFeedback{{ID: 2, UserID: userID, RecommendationItemID: itemID, ClosedReason: "superseded"}}, nil
	}
	response := performUserPathControllerRequest(http.MethodGet, "/recommendations/items/:itemId/feedback", "/recommendations/items/12/feedback", &auth.User{ID: 91}, GetRecommendationItemFeedback)
	if response.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", response.Code, response.Body.String())
	}
	data := decodeResponse(t, response)["Data"].(map[string]interface{})
	if data["current"] == nil || len(data["history"].([]interface{})) != 1 {
		t.Fatalf("feedback data = %#v", data)
	}

	resetUserRecommendationPreferences = func(userID uint) (*recommendation.UserRecommendationProfile, error) {
		return &recommendation.UserRecommendationProfile{UserID: userID, ProfileVersion: 4}, nil
	}
	resetResponse := performUserControllerRequest(http.MethodPost, "/recommendations/preferences/reset", nil, &auth.User{ID: 91}, ResetRecommendationPreferences)
	if resetResponse.Code != http.StatusOK {
		t.Fatalf("reset status = %d body=%s", resetResponse.Code, resetResponse.Body.String())
	}
}
