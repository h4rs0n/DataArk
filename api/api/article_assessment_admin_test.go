package api

import (
	"DataArk/assessmenteval"
	"DataArk/auth"
	"DataArk/discovery"
	"context"
	"net/http"
	"testing"
)

func TestArticleAssessmentWorkflowIsOwnerOnlyAndCreatesServerSideRun(t *testing.T) {
	oldGet := getArticleAssessmentWorkflow
	oldCreate := createArticleAssessmentWorkflow
	t.Cleanup(func() {
		getArticleAssessmentWorkflow = oldGet
		createArticleAssessmentWorkflow = oldCreate
	})
	getCalls, createCalls := 0, 0
	getArticleAssessmentWorkflow = func() (assessmenteval.WorkflowSummary, error) {
		getCalls++
		return assessmenteval.WorkflowSummary{Exists: true, RunID: 9, Status: assessmenteval.WorkflowStatusPassOne, PassOne: assessmenteval.WorkflowProgress{Total: 120, Labeled: 4}}, nil
	}
	createArticleAssessmentWorkflow = func(userID uint) (assessmenteval.WorkflowSummary, error) {
		createCalls++
		if userID != 1 {
			t.Fatalf("creator user ID = %d", userID)
		}
		return assessmenteval.WorkflowSummary{Exists: true, RunID: 10, Status: assessmenteval.WorkflowStatusPassOne, PassOne: assessmenteval.WorkflowProgress{Total: 120}}, nil
	}
	member := &auth.User{ID: 2, Role: auth.UserRoleMember}
	owner := &auth.User{ID: 1, Role: auth.UserRoleOwner}
	if response := performUserControllerRequest(http.MethodGet, "/admin/recommendations/article-assessment-workflow", nil, member, GetArticleAssessmentWorkflow); response.Code != http.StatusForbidden {
		t.Fatalf("member GET status = %d", response.Code)
	}
	if response := performUserControllerRequest(http.MethodGet, "/admin/recommendations/article-assessment-workflow", nil, owner, GetArticleAssessmentWorkflow); response.Code != http.StatusOK || getCalls != 1 {
		t.Fatalf("owner GET status=%d calls=%d", response.Code, getCalls)
	}
	response := performUserControllerRequest(http.MethodPost, "/admin/recommendations/article-assessment-workflow/runs", []byte(`{}`), owner, CreateArticleAssessmentWorkflow)
	if response.Code != http.StatusCreated || createCalls != 1 {
		t.Fatalf("owner create status=%d calls=%d body=%s", response.Code, createCalls, response.Body.String())
	}
	data := decodeResponse(t, response)["Data"].(map[string]interface{})
	if data["runId"] != float64(10) || data["status"] != assessmenteval.WorkflowStatusPassOne {
		t.Fatalf("workflow response = %#v", data)
	}
}

func TestArticleAssessmentBackfillRequiresOwnerAndSupportsDryRun(t *testing.T) {
	oldPrepare := prepareArticleAssessmentBackfill
	t.Cleanup(func() { prepareArticleAssessmentBackfill = oldPrepare })
	calls := 0
	prepareArticleAssessmentBackfill = func(_ context.Context, options discovery.ArticleAssessmentBatchOptions) (discovery.ArticleAssessmentBatchResult, error) {
		calls++
		if options.Limit != 120 || !options.DryRun || options.RetryFailures {
			t.Fatalf("options = %#v", options)
		}
		return discovery.ArticleAssessmentBatchResult{PolicyVersion: "article-value-v3", Selected: 120, Reactivated: 7, DryRun: true}, nil
	}
	body := []byte(`{"limit":120,"dryRun":true}`)
	member := performUserControllerRequest(http.MethodPost, "/admin/discovery/article-assessments/backfill", body, &auth.User{ID: 2, Role: auth.UserRoleMember}, BackfillArticleAssessments)
	if member.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("member status=%d calls=%d", member.Code, calls)
	}
	owner := performUserControllerRequest(http.MethodPost, "/admin/discovery/article-assessments/backfill", body, &auth.User{ID: 1, Role: auth.UserRoleOwner}, BackfillArticleAssessments)
	if owner.Code != http.StatusOK || calls != 1 {
		t.Fatalf("owner status=%d calls=%d", owner.Code, calls)
	}
	data := decodeResponse(t, owner)["Data"].(map[string]interface{})
	if data["selected"] != float64(120) || data["reactivated"] != float64(7) || data["dryRun"] != true {
		t.Fatalf("response data = %#v", data)
	}
}

func TestArticleAssessmentRollbackRequiresOwner(t *testing.T) {
	oldRollback := rollbackArticleAssessments
	t.Cleanup(func() { rollbackArticleAssessments = oldRollback })
	calls := 0
	rollbackArticleAssessments = func(_ context.Context, options discovery.ArticleAssessmentBatchOptions) (discovery.ArticleAssessmentBatchResult, error) {
		calls++
		if options.Limit != 250 || options.DryRun {
			t.Fatalf("options = %#v", options)
		}
		return discovery.ArticleAssessmentBatchResult{PolicyVersion: "article-value-v3", Selected: 2, Reactivated: 2}, nil
	}
	body := []byte(`{"limit":250}`)
	member := performUserControllerRequest(http.MethodPost, "/admin/discovery/article-assessments/rollback", body, &auth.User{ID: 2, Role: auth.UserRoleMember}, RollbackArticleAssessments)
	if member.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("member status=%d calls=%d", member.Code, calls)
	}
	owner := performUserControllerRequest(http.MethodPost, "/admin/discovery/article-assessments/rollback", body, &auth.User{ID: 1, Role: auth.UserRoleOwner}, RollbackArticleAssessments)
	if owner.Code != http.StatusAccepted || calls != 1 {
		t.Fatalf("owner status=%d calls=%d", owner.Code, calls)
	}
}
