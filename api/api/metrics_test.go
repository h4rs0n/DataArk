package api

import (
	"DataArk/auth"
	"DataArk/recommendation"
	"net/http"
	"testing"
	"time"
)

func TestObservabilityM16MetricsRequireOwner(t *testing.T) {
	oldMetrics := getAdminProductMetrics
	t.Cleanup(func() { getAdminProductMetrics = oldMetrics })
	called := 0
	getAdminProductMetrics = func(time.Time) (*recommendation.AdminProductMetrics, error) {
		called++
		return &recommendation.AdminProductMetrics{}, nil
	}
	member := performUserControllerRequest(http.MethodGet, "/admin/recommendations/metrics", nil, &auth.User{ID: 2, Role: auth.UserRoleMember}, GetAdminProductMetrics)
	if member.Code != http.StatusForbidden || called != 0 {
		t.Fatalf("member status=%d calls=%d", member.Code, called)
	}
	owner := performUserControllerRequest(http.MethodGet, "/admin/recommendations/metrics", nil, &auth.User{ID: 1, Role: auth.UserRoleOwner}, GetAdminProductMetrics)
	if owner.Code != http.StatusOK || called != 1 {
		t.Fatalf("owner status=%d calls=%d", owner.Code, called)
	}
}
