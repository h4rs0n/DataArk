package api

import (
	"DataArk/auth"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDedupeOperationsRequireOwner(t *testing.T) {
	member := &auth.User{ID: 2, Role: auth.UserRoleMember}
	for _, test := range []struct {
		method, route, path string
		handler             gin.HandlerFunc
	}{
		{http.MethodPost, "/admin/discovery/dedupe/runs", "/admin/discovery/dedupe/runs", CreateDedupeRun},
		{http.MethodGet, "/admin/discovery/dedupe/runs/:id", "/admin/discovery/dedupe/runs/1", GetDedupeRun},
		{http.MethodPost, "/admin/discovery/dedupe/runs/:id/apply", "/admin/discovery/dedupe/runs/1/apply", ApplyDedupeRun},
		{http.MethodPost, "/admin/discovery/dedupe/runs/:id/rollback", "/admin/discovery/dedupe/runs/1/rollback", RollbackDedupeRun},
		{http.MethodGet, "/admin/discovery/dedupe/clusters/:id", "/admin/discovery/dedupe/clusters/idx-1-1", GetDedupeCluster},
	} {
		response := performUserPathControllerRequest(test.method, test.route, test.path, member, test.handler)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d", test.method, test.path, response.Code)
		}
	}
}
func TestDedupeInvalidRunID(t *testing.T) {
	owner := &auth.User{ID: 1, Role: auth.UserRoleOwner}
	for _, id := range []string{"0", "-1", "abc", "4294967296"} {
		response := performUserPathControllerRequest(http.MethodGet, "/admin/discovery/dedupe/runs/:id", "/admin/discovery/dedupe/runs/"+id, owner, GetDedupeRun)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("id=%s status=%d", id, response.Code)
		}
	}
}
