package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRemovedArticleAssessmentWorkflowRoutesReturnNotFound(t *testing.T) {
	router := gin.New()
	registerAPIRoutes(router, &AuthController{})
	const base = "/api/admin/recommendations/article-assessment-workflow"
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, base},
		{http.MethodPost, base + "/runs"},
		{http.MethodGet, base + "/runs/1/items/1/0"},
		{http.MethodPut, base + "/runs/1/labels/1/sample"},
		{http.MethodPut, base + "/runs/1/skips/1/sample"},
		{http.MethodPost, base + "/runs/1/advance"},
		{http.MethodPost, base + "/runs/1/evaluate"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", response.Code)
			}
		})
	}
}
