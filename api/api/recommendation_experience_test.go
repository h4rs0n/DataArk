package api

import (
	"DataArk/auth"
	"DataArk/recommendation"
	"net/http"
	"testing"
)

func TestRecommendationExperienceM15ContextUsesAuthenticatedUser(t *testing.T) {
	oldContext := getRecommendationItemContext
	t.Cleanup(func() { getRecommendationItemContext = oldContext })
	getRecommendationItemContext = func(userID uint, itemID uint) (*recommendation.RecommendationItemContext, error) {
		if userID != 920 || itemID != 33 {
			t.Fatalf("identity = %d/%d", userID, itemID)
		}
		return &recommendation.RecommendationItemContext{Item: recommendation.RecommendationItem{ID: itemID, UserID: userID}}, nil
	}
	response := performUserPathControllerRequest(http.MethodGet, "/recommendations/items/:itemId/context", "/recommendations/items/33/context", &auth.User{ID: 920}, GetRecommendationItemContext)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestRecommendationExperienceM15AuthCheckerReturnsRole(t *testing.T) {
	controller := &AuthController{}
	response := performUserControllerRequest(http.MethodGet, "/authChecker", nil, &auth.User{ID: 1, Username: "admin", Role: auth.UserRoleOwner}, controller.AuthChecker)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	data := decodeResponse(t, response)["Data"].(map[string]interface{})
	if data["role"] != auth.UserRoleOwner {
		t.Fatalf("auth data = %#v", data)
	}
}
