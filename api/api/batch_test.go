package api

import (
	"DataArk/auth"
	"context"
	"net/http"
	"testing"
)

func TestAssessmentBatchEndpointsAreOwnerOnly(t *testing.T) {
	member := &auth.User{ID: 2, Role: auth.UserRoleMember}
	res := performUserPathControllerRequest(http.MethodGet, "/admin/assessment/batches", "/admin/assessment/batches", member, GetAssessmentBatches)
	if res.Code != http.StatusForbidden {
		t.Fatal(res.Code)
	}
	res = performUserPathControllerRequest(http.MethodGet, "/admin/assessment/batches/:id", "/admin/assessment/batches/1", member, GetAssessmentBatch)
	if res.Code != http.StatusForbidden {
		t.Fatal(res.Code)
	}
	owner := &auth.User{ID: 1, Role: auth.UserRoleOwner}
	res = performUserPathControllerRequest(http.MethodGet, "/admin/assessment/batches/:id", "/admin/assessment/batches/invalid", owner, GetAssessmentBatch)
	if res.Code != http.StatusBadRequest {
		t.Fatal(res.Code)
	}
	res = performUserPathControllerRequest(http.MethodGet, "/admin/assessment/batches", "/admin/assessment/batches?beforeId=bad", owner, GetAssessmentBatches)
	if res.Code != http.StatusBadRequest {
		t.Fatal(res.Code)
	}
}

func TestBatchCleanupResumeIsOwnerOnly(t *testing.T) {
	old := resumeAssessmentBatchCleanup
	defer func() { resumeAssessmentBatchCleanup = old }()
	calls := 0
	resumeAssessmentBatchCleanup = func(context.Context) error { calls++; return nil }
	path := "/admin/assessment/batches/cleanup/resume"
	member := performUserPathControllerRequest(http.MethodPost, path, path, &auth.User{ID: 2, Role: auth.UserRoleMember}, ResumeAssessmentBatchCleanup)
	if member.Code != http.StatusForbidden || calls != 0 {
		t.Fatal(member.Code, calls)
	}
	owner := performUserPathControllerRequest(http.MethodPost, path, path, &auth.User{ID: 1, Role: auth.UserRoleOwner}, ResumeAssessmentBatchCleanup)
	if owner.Code != http.StatusAccepted || calls != 1 {
		t.Fatal(owner.Code, calls)
	}
}
