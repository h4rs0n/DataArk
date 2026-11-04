package api

import (
	"DataArk/auth"
	"net/http"
	"testing"
)

func TestSearchSyncStatusRequiresOwner(t *testing.T) {
	response := performUserControllerRequest(http.MethodGet, "/admin/search/status", nil, &auth.User{ID: 2, Role: auth.UserRoleMember}, GetSearchSyncStatus)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
