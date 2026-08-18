package api

import (
	"DataArk/auth"
	"DataArk/jobqueue"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestDiscoveryCrawlQueueRequiresOwnerAndReturnsSafeSnapshot(t *testing.T) {
	oldGet := getDiscoveryCrawlQueue
	t.Cleanup(func() { getDiscoveryCrawlQueue = oldGet })
	getDiscoveryCrawlQueue = func(_ context.Context, limit int) (*jobqueue.CrawlQueueSnapshot, error) {
		if limit != 25 {
			t.Fatalf("limit = %d, want 25", limit)
		}
		return &jobqueue.CrawlQueueSnapshot{
			Mode: "automatic", State: "waiting", CanRun: false, UpdatedAt: time.Now(),
			Counts: jobqueue.CrawlQueueCounts{Pending: 1},
			Tasks:  []jobqueue.CrawlQueueTask{{ID: "7", Kind: jobqueue.FetchSourceJobKind, TargetType: "source", TargetID: 9, Status: "pending"}},
		}, nil
	}

	member := performUserPathControllerRequest(http.MethodGet, "/admin/discovery/crawl-queue", "/admin/discovery/crawl-queue?limit=25", &auth.User{ID: 2, Role: auth.UserRoleMember}, GetDiscoveryCrawlQueue)
	if member.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", member.Code)
	}
	owner := performUserPathControllerRequest(http.MethodGet, "/admin/discovery/crawl-queue", "/admin/discovery/crawl-queue?limit=25", &auth.User{ID: 1, Role: auth.UserRoleOwner}, GetDiscoveryCrawlQueue)
	if owner.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200", owner.Code)
	}
	payload := decodeResponse(t, owner)
	data := payload["Data"].(map[string]interface{})
	if data["mode"] != "automatic" || data["state"] != "waiting" || data["canRun"] != false {
		t.Fatalf("unexpected data: %#v", data)
	}
}

func TestRunDiscoveryCrawlQueueIsOwnerOnly(t *testing.T) {
	oldRun := runDiscoveryCrawlQueue
	t.Cleanup(func() { runDiscoveryCrawlQueue = oldRun })
	calls := 0
	runDiscoveryCrawlQueue = func(context.Context) (*jobqueue.CrawlQueueSnapshot, error) {
		calls++
		return &jobqueue.CrawlQueueSnapshot{Mode: "automatic", State: "waiting"}, nil
	}

	member := performUserControllerRequest(http.MethodPost, "/admin/discovery/crawl-queue/run", nil, &auth.User{ID: 2, Role: auth.UserRoleMember}, RunDiscoveryCrawlQueue)
	if member.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("member status=%d calls=%d", member.Code, calls)
	}
	owner := performUserControllerRequest(http.MethodPost, "/admin/discovery/crawl-queue/run", nil, &auth.User{ID: 1, Role: auth.UserRoleOwner}, RunDiscoveryCrawlQueue)
	if owner.Code != http.StatusAccepted || calls != 1 {
		t.Fatalf("owner status=%d calls=%d", owner.Code, calls)
	}

	runDiscoveryCrawlQueue = func(context.Context) (*jobqueue.CrawlQueueSnapshot, error) {
		return nil, errors.New("queue offline")
	}
	failure := performUserControllerRequest(http.MethodPost, "/admin/discovery/crawl-queue/run", nil, &auth.User{ID: 1, Role: auth.UserRoleOwner}, RunDiscoveryCrawlQueue)
	if failure.Code != http.StatusServiceUnavailable {
		t.Fatalf("failure status = %d, want 503", failure.Code)
	}
}

func TestAssessmentQueueRequiresOwnerAndManualRun(t *testing.T) {
	oldGet := getAssessmentQueue
	oldRun := runAssessmentQueue
	t.Cleanup(func() {
		getAssessmentQueue = oldGet
		runAssessmentQueue = oldRun
	})
	getAssessmentQueue = func(_ context.Context, limit int) (*jobqueue.CrawlQueueSnapshot, error) {
		if limit != 25 {
			t.Fatalf("limit = %d, want 25", limit)
		}
		return &jobqueue.CrawlQueueSnapshot{
			Mode: "manual", State: "waiting", CanRun: true, UpdatedAt: time.Now(),
			Counts: jobqueue.CrawlQueueCounts{Pending: 2},
			Tasks:  []jobqueue.CrawlQueueTask{{ID: "9", Kind: jobqueue.AssessArticleJobKind, TargetType: "candidate", TargetID: 4, Status: "pending"}},
		}, nil
	}
	member := performUserPathControllerRequest(http.MethodGet, "/admin/assessment/queue", "/admin/assessment/queue?limit=25", &auth.User{ID: 2, Role: auth.UserRoleMember}, GetAssessmentQueue)
	if member.Code != http.StatusForbidden {
		t.Fatalf("member status = %d, want 403", member.Code)
	}
	owner := performUserPathControllerRequest(http.MethodGet, "/admin/assessment/queue", "/admin/assessment/queue?limit=25", &auth.User{ID: 1, Role: auth.UserRoleOwner}, GetAssessmentQueue)
	if owner.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200", owner.Code)
	}
	data := decodeResponse(t, owner)["Data"].(map[string]interface{})
	if data["mode"] != "manual" || data["canRun"] != true {
		t.Fatalf("unexpected data: %#v", data)
	}

	calls := 0
	runAssessmentQueue = func(context.Context) (*jobqueue.CrawlQueueSnapshot, error) {
		calls++
		return &jobqueue.CrawlQueueSnapshot{Mode: "manual", State: "running"}, nil
	}
	memberRun := performUserControllerRequest(http.MethodPost, "/admin/assessment/queue/run", nil, &auth.User{ID: 2, Role: auth.UserRoleMember}, RunAssessmentQueue)
	if memberRun.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("member run status=%d calls=%d", memberRun.Code, calls)
	}
	ownerRun := performUserControllerRequest(http.MethodPost, "/admin/assessment/queue/run", nil, &auth.User{ID: 1, Role: auth.UserRoleOwner}, RunAssessmentQueue)
	if ownerRun.Code != http.StatusAccepted || calls != 1 {
		t.Fatalf("owner run status=%d calls=%d", ownerRun.Code, calls)
	}
}
