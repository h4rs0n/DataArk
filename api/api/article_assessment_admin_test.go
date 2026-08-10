package api

import (
	"DataArk/auth"
	"DataArk/discovery"
	"context"
	"net/http"
	"testing"
)

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
