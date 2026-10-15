package assessment

import (
	"DataArk/config"
	"DataArk/jobqueue"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBatchProviderStructuredHTTPErrorsAreRedacted(t *testing.T) {
	for _, code := range []int{401, 429} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
				io.WriteString(w, `{"error":{"code":"InvalidApiKey","message":"fixture-secret at https://private.example/path?token=secret","request_body":"PRIVATE ARTICLE"}}`)
			}))
			defer server.Close()
			p := BailianBatchProvider{BaseURL: server.URL, APIKey: "fixture-secret", Client: server.Client()}
			_, err := p.Retrieve(context.Background(), "remote")
			var h batchHTTPError
			if !errors.As(err, &h) || h.Status != code || h.Code != "InvalidApiKey" {
				t.Fatal(err)
			}
			for _, secret := range []string{"fixture-secret", "private.example", "PRIVATE ARTICLE"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("unsafe diagnostics", err)
				}
			}
		})
	}
}

func TestBatchRemoteValidationErrorsSurviveResultFinalization(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	ctx := context.Background()
	_ = StartBatchRun(ctx)
	reachSubmittedBatch(t, p)
	var b Batch
	db.First(&b)
	problems := json.RawMessage(`{"data":[{"code":"invalid_json","message":"invalid input on line 1","line":1}]}`)
	if err := saveRemote(&b, RemoteBatch{ID: b.RemoteID, Status: "failed", Errors: problems}); err != nil {
		t.Fatal(err)
	}
	batchStep(t)
	db.First(&b, b.ID)
	var item BatchItem
	db.First(&item)
	if b.Phase != "done" || !strings.Contains(b.Error, "invalid_json") || !strings.Contains(item.Error, "line 1") || item.ActiveIdentity != nil {
		t.Fatal(b, item)
	}
}

func TestBatchErrorOnlyResultsPauseAndResumeWithoutRepeatingInference(t *testing.T) {
	for _, code := range []string{"InvalidApiKey", "invalid_api_key", "Throttling.RateQuota", "Throttling.AllocationQuota", "insufficient_quota", "429"} {
		t.Run(code, func(t *testing.T) {
			p, _, _ := setupBatchFixture(t)
			ctx := context.Background()
			q, _ := jobqueue.Default()
			c := createAssessmentCandidate(t, "Error file", "https://batch.invalid/after-error", "Later result", strings.Repeat("later evidence ", 100), time.Now())
			if err := q.EnqueueAssessArticle(ctx, c.MaterialID, "1"); err != nil {
				t.Fatal(err)
			}
			_ = StartBatchRun(ctx)
			reachSubmittedBatch(t, p)
			var items []BatchItem
			db.Order("id").Find(&items)
			p.output = fmt.Sprintf(`{"custom_id":%q,"response":null,"error":{"code":%q,"message":"provider diagnostic"}}`+"\n", items[0].CustomID, code) + batchResult(items[1].CustomID, validBatchAssessment)
			batchStep(t)
			batchStep(t)
			var run BatchRun
			db.First(&run)
			db.First(&items[0], items[0].ID)
			if run.Status != "paused" || run.ErrorCode != batchProblemStatus(code) || !strings.Contains(items[0].Error, code) {
				t.Fatal(run, items[0])
			}
			requests := p.requests
			if err := ProcessBatchTick(ctx); err != nil {
				t.Fatal(err)
			}
			if requests != p.requests {
				t.Fatal("paused output continued provider requests")
			}
			_, _ = ResumeBatchRun(ctx)
			batchStep(t)
			batchStep(t)
			db.First(&items[1], items[1].ID)
			var calls int64
			db.Model(&LLMCall{}).Count(&calls)
			if items[1].Status != "succeeded" || p.creates != 1 || calls != 2 {
				t.Fatal("resume repeated results", items[1], p.creates, calls)
			}
		})
	}
}

func TestBatchValidationAuthErrorPreservesRemoteIdentityOnResume(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	ctx := context.Background()
	p.remoteErrors = json.RawMessage(`{"data":[{"code":"InvalidApiKey","message":"check workspace credentials"}]}`)
	_ = StartBatchRun(ctx)
	reachSubmittedBatch(t, p)
	var run BatchRun
	db.First(&run)
	var b Batch
	db.First(&b)
	if run.Status != "paused" || b.Phase != "importing" || b.RemoteID == "" {
		t.Fatal(run, b)
	}
	_, _ = ResumeBatchRun(ctx)
	batchStep(t)
	db.First(&b, b.ID)
	if b.Phase != "done" || b.RemoteID != "remote-fixture" || p.creates != 1 {
		t.Fatal("remote validation failure resubmitted", b, p.creates)
	}
}

func TestBatchMissingRemoteLookupPausesAfterBoundedAttempts(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	ctx := context.Background()
	_ = StartBatchRun(ctx)
	reachSubmittedBatch(t, p)
	p.operationErrors = map[string]error{"retrieve": batchHTTPError{Status: 404, Code: "NotFound"}}
	for n := 0; n < 3; n++ {
		batchStep(t)
	}
	var run BatchRun
	db.First(&run)
	var b Batch
	db.First(&b)
	if run.Status != "paused" || run.ErrorCode != 404 || b.Phase != "submitted" || run.ActiveKey == nil {
		t.Fatal(run, b)
	}
	requests := p.requests
	if err := RecoverBatchJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ProcessBatchTick(ctx); err != nil {
		t.Fatal(err)
	}
	if requests != p.requests {
		t.Fatal("missing remote still polled")
	}
	_, _ = ResumeBatchRun(ctx)
	p.operationErrors = nil
	batchStep(t)
	if p.creates != 1 {
		t.Fatal("lookup resume resubmitted batch")
	}
}

func TestBatch404OutsideQueryWindowEndsOnlyUnfinishedItems(t *testing.T) {
	p, _, c := setupBatchFixture(t)
	ctx := context.Background()
	q, _ := jobqueue.Default()
	later := createAssessmentCandidate(t, "Expired request", "https://batch.invalid/missing-expired", "Missing result", strings.Repeat("missing evidence ", 100), time.Now())
	if err := q.EnqueueAssessArticle(ctx, later.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	_ = StartBatchRun(ctx)
	reachSubmittedBatch(t, p)
	var b Batch
	db.First(&b)
	var succeeded BatchItem
	db.Where("material_id=?", c.MaterialID).First(&succeeded)
	if err := importBatchStream(ctx, mustLoadBatchRun(t), &b, strings.NewReader(batchResult(succeeded.CustomID, validBatchAssessment))); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := db.Model(&b).Updates(map[string]any{"created_at": old, "submitted_at": old}).Error; err != nil {
		t.Fatal(err)
	}
	p.operationErrors = map[string]error{"retrieve": batchHTTPError{Status: 404}}
	batchStep(t)
	batchStep(t)
	db.First(&b, b.ID)
	var run BatchRun
	db.First(&run)
	var item BatchItem
	db.Where("material_id=?", later.MaterialID).First(&item)
	if b.Phase != "done" || run.ActiveKey != nil || item.Status != "failed" || item.ActiveIdentity != nil || !strings.Contains(item.Error, "30-day") {
		t.Fatal(run, b, item)
	}
	db.First(&succeeded, succeeded.ID)
	if succeeded.Status != "succeeded" || succeeded.AssessmentID == nil {
		t.Fatal("expired lookup overwrote an imported result", succeeded)
	}
}

func mustLoadBatchRun(t *testing.T) *BatchRun {
	t.Helper()
	var run BatchRun
	if err := db.First(&run).Error; err != nil {
		t.Fatal(err)
	}
	return &run
}

func TestBatchCleanupAuthFailureIsDurableAndManuallyRetryable(t *testing.T) {
	for _, code := range []int{401, 429} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			p, _, _ := setupBatchFixture(t)
			verifyCleanupAuthPause(t, p, code)
		})
	}
}

// Shared with the real Postgres/River integration test.
func verifyCleanupAuthPause(t *testing.T, p *fixtureBatchProvider, code int) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-8 * 24 * time.Hour)
	run := BatchRun{Status: "rolled_back", BaseURL: config.LLMBATCHBASEURL, CreatedAt: old, FinishedAt: &old}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	b := Batch{RunID: run.ID, UUID: fmt.Sprintf("cleanup-pause-%d-%d", run.ID, code), Phase: "done", InputFileID: "file-cleanup", Error: "validation diagnostic", FinishedAt: &old, CreatedAt: old}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	before := p.requests
	p.operationErrors = map[string]error{"delete": batchHTTPError{Status: code, Code: "provider-code", Message: "provider detail"}}
	if err := ProcessBatchTick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := RecoverBatchJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ProcessBatchTick(ctx); err != nil {
		t.Fatal(err)
	}
	if p.requests != before+1 {
		t.Fatal("cleanup retried automatically", before, p.requests)
	}
	var persisted BatchRun
	db.First(&persisted, run.ID)
	if persisted.Status != "rolled_back" || persisted.ResumeStatus != "cleanup" || persisted.ErrorCode != code || persisted.PausedAt == nil {
		t.Fatal(persisted)
	}
	view, err := DecorateQueue(ctx, &jobqueue.CrawlQueueSnapshot{})
	if err != nil || view.BatchCleanup == nil || view.BatchCleanup.ErrorCode != code || view.CanRun {
		t.Fatal(view, err)
	}
	if err := StartBatchRun(ctx); err == nil {
		t.Fatal("cleanup auth failure started inference")
	}
	p.operationErrors = nil
	_ = ResumeBatchCleanup(ctx) // fixture intentionally has no tick handler
	if err := ProcessBatchTick(ctx); err != nil {
		t.Fatal(err)
	}
	db.First(&b, b.ID)
	persisted = BatchRun{}
	db.First(&persisted, run.ID)
	if b.CleanedAt == nil || b.Error != "validation diagnostic" || persisted.PausedAt != nil || persisted.Status != "rolled_back" {
		t.Fatal(b, persisted)
	}
}
