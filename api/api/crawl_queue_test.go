package api

import (
	"DataArk/auth"
	"DataArk/dedupe"
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

func TestSetDiscoveryCrawlQueueSettings(t *testing.T) {
	oldSet := setDiscoveryCrawlAutoStart
	t.Cleanup(func() { setDiscoveryCrawlAutoStart = oldSet })
	owner := &auth.User{ID: 1, Role: auth.UserRoleOwner}
	member := &auth.User{ID: 2, Role: auth.UserRoleMember}
	calls := 0
	var enabled bool
	setDiscoveryCrawlAutoStart = func(_ context.Context, value bool) (*jobqueue.CrawlQueueSnapshot, error) {
		calls++
		enabled = value
		mode := "manual"
		if value {
			mode = "automatic"
		}
		return &jobqueue.CrawlQueueSnapshot{Mode: mode, State: "waiting", CanRun: !value}, nil
	}
	path := "/admin/discovery/crawl-queue/settings"
	if response := performUserControllerRequest(http.MethodPut, path, []byte(`{"autoStart":true}`), member, SetDiscoveryCrawlQueueSettings); response.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("member status=%d calls=%d", response.Code, calls)
	}
	for _, body := range []string{`{}`, `{"autoStart":null}`, `{"autoStart":"false"}`, `{"autoStart":0}`, `{`} {
		response := performUserControllerRequest(http.MethodPut, path, []byte(body), owner, SetDiscoveryCrawlQueueSettings)
		if response.Code != http.StatusBadRequest || calls != 0 {
			t.Fatalf("body=%s status=%d calls=%d", body, response.Code, calls)
		}
	}
	for _, body := range []string{`{"autoStart":true}`, `{"autoStart":false}`} {
		response := performUserControllerRequest(http.MethodPut, path, []byte(body), owner, SetDiscoveryCrawlQueueSettings)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		data := decodeResponse(t, response)["Data"].(map[string]interface{})
		if data["canRun"] != !enabled {
			t.Fatalf("snapshot=%#v", data)
		}
	}
	setDiscoveryCrawlAutoStart = func(context.Context, bool) (*jobqueue.CrawlQueueSnapshot, error) {
		return nil, errors.New("queue offline")
	}
	response := performUserControllerRequest(http.MethodPut, path, []byte(`{"autoStart":false}`), owner, SetDiscoveryCrawlQueueSettings)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestFetchDiscoverySourceOnlyRegistersTask(t *testing.T) {
	oldRequest := requestDiscoverySourceFetch
	t.Cleanup(func() { requestDiscoverySourceFetch = oldRequest })
	calls := 0
	requestDiscoverySourceFetch = func(_ context.Context, id uint) error {
		calls++
		if id != 7 {
			t.Fatalf("source id=%d", id)
		}
		return nil
	}
	path := "/discovery/sources/:id/fetch"
	target := "/discovery/sources/7/fetch"
	member := performUserPathControllerRequest(http.MethodPost, path, target, &auth.User{ID: 2, Role: auth.UserRoleMember}, FetchDiscoverySource)
	if member.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("member status=%d calls=%d", member.Code, calls)
	}
	owner := performUserPathControllerRequest(http.MethodPost, path, target, &auth.User{ID: 1, Role: auth.UserRoleOwner}, FetchDiscoverySource)
	if owner.Code != http.StatusAccepted || calls != 1 {
		t.Fatalf("status=%d calls=%d", owner.Code, calls)
	}
	data := decodeResponse(t, owner)["Data"].(map[string]interface{})
	if data["queued"] != true || data["sourceId"] != float64(7) {
		t.Fatalf("data=%#v", data)
	}
	requestDiscoverySourceFetch = func(context.Context, uint) error { return errors.New("enqueue failed") }
	failure := performUserPathControllerRequest(http.MethodPost, path, target, &auth.User{ID: 1, Role: auth.UserRoleOwner}, FetchDiscoverySource)
	if failure.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", failure.Code)
	}
}

func TestAssessmentQueueRequiresOwnerAndManualRun(t *testing.T) {
	oldGet := getAssessmentQueue
	oldRun := runAssessmentQueue
	t.Cleanup(func() {
		getAssessmentQueue = oldGet
		runAssessmentQueue = oldRun
	})
	getAssessmentQueue = func(_ context.Context, limit int) (*jobqueue.CrawlQueueSnapshot, *dedupe.Run, error) {
		if limit != 25 {
			t.Fatalf("limit = %d, want 25", limit)
		}
		return &jobqueue.CrawlQueueSnapshot{
			Mode: "manual", State: "waiting", CanRun: true, UpdatedAt: time.Now(),
			Counts: jobqueue.CrawlQueueCounts{Pending: 2},
			Tasks:  []jobqueue.CrawlQueueTask{{ID: "9", Kind: jobqueue.AssessArticleJobKind, TargetType: "candidate", TargetID: 4, Status: "pending"}},
		}, nil, nil
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

	getAssessmentQueue = func(_ context.Context, _ int) (*jobqueue.CrawlQueueSnapshot, *dedupe.Run, error) {
		return &jobqueue.CrawlQueueSnapshot{Mode: "manual", State: "waiting"}, &dedupe.Run{ID: 3, Status: "indexing", AutoApply: true, HighWaterID: 100, Cursor: 40, Indexed: 38}, nil
	}
	deferredGet := performUserPathControllerRequest(http.MethodGet, "/admin/assessment/queue", "/admin/assessment/queue?limit=25", &auth.User{ID: 1, Role: auth.UserRoleOwner}, GetAssessmentQueue)
	if deferredGet.Code != http.StatusOK {
		t.Fatalf("deferred get status = %d, want 200", deferredGet.Code)
	}
	deferredGetData := decodeResponse(t, deferredGet)["Data"].(map[string]interface{})
	snapshot, ok := deferredGetData["snapshot"].(map[string]interface{})
	if !ok || snapshot["mode"] != "manual" {
		t.Fatalf("deferred get missing snapshot: %#v", deferredGetData)
	}
	buildRun, ok := deferredGetData["dedupeRun"].(map[string]interface{})
	if !ok || buildRun["id"] != float64(3) || buildRun["status"] != "indexing" || buildRun["highWaterId"] != float64(100) || buildRun["cursor"] != float64(40) || buildRun["indexed"] != float64(38) {
		t.Fatalf("unexpected deferred get run: %#v", deferredGetData)
	}

	calls := 0
	runAssessmentQueue = func(context.Context) (*jobqueue.CrawlQueueSnapshot, *dedupe.Run, error) {
		calls++
		return &jobqueue.CrawlQueueSnapshot{Mode: "manual", State: "running"}, nil, nil
	}
	memberRun := performUserControllerRequest(http.MethodPost, "/admin/assessment/queue/run", nil, &auth.User{ID: 2, Role: auth.UserRoleMember}, RunAssessmentQueue)
	if memberRun.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("member run status=%d calls=%d", memberRun.Code, calls)
	}
	ownerRun := performUserControllerRequest(http.MethodPost, "/admin/assessment/queue/run", nil, &auth.User{ID: 1, Role: auth.UserRoleOwner}, RunAssessmentQueue)
	if ownerRun.Code != http.StatusAccepted || calls != 1 {
		t.Fatalf("owner run status=%d calls=%d", ownerRun.Code, calls)
	}

	runAssessmentQueue = func(context.Context) (*jobqueue.CrawlQueueSnapshot, *dedupe.Run, error) {
		return &jobqueue.CrawlQueueSnapshot{Mode: "manual", State: "waiting"}, &dedupe.Run{ID: 7, Status: "indexing", AutoApply: true}, nil
	}
	deferred := performUserControllerRequest(http.MethodPost, "/admin/assessment/queue/run", nil, &auth.User{ID: 1, Role: auth.UserRoleOwner}, RunAssessmentQueue)
	if deferred.Code != http.StatusAccepted {
		t.Fatalf("deferred status = %d, want 202", deferred.Code)
	}
	deferredData := decodeResponse(t, deferred)["Data"].(map[string]interface{})
	dedupeRun, ok := deferredData["dedupeRun"].(map[string]interface{})
	if !ok || dedupeRun["id"] != float64(7) || dedupeRun["autoApply"] != true || dedupeRun["status"] != "indexing" {
		t.Fatalf("unexpected deferred data: %#v", deferredData)
	}
	if deferredData["snapshot"] == nil {
		t.Fatalf("deferred response missing snapshot: %#v", deferredData)
	}
}
