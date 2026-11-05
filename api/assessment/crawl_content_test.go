package assessment

import (
	"DataArk/config"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"context"
	"errors"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestAssessmentUsesContentDespiteFailedDiscoveryEntrypoint(t *testing.T) {
	setupAssessmentDB(t)
	candidate := createAssessmentCandidate(t, "Feed", "https://failed.example/article", "Accepted article", strings.Repeat("measurement 42 substantive evidence ", 50), time.Now())
	if err := db.Model(&discovery.DiscoveryCandidate{}).Where("id=?", candidate.ID).Updates(map[string]any{"processing_state": "failed", "next_processing_at": time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	queue := &recoveryQueue{}
	if err := RecoverDueJobs(context.Background(), queue, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(queue.targets) != 1 {
		t.Fatalf("fetch retry delayed assessment: %+v", queue.targets)
	}
	assessor := fixtureArticleAssessor{name: "content-fixture", version: "1", result: ArticleAssessmentResult{Quality: .9, Depth: .8, Evergreen: .7, Confidence: .9, Reasons: []string{"Evidence supports the conclusion"}}}
	if err := AssessMaterial(context.Background(), candidate.MaterialID, "1", assessor); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&ArticleAssessment{}).Where("material_id=?", candidate.MaterialID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("retained content not assessed: %d %v", count, err)
	}
}

func TestBatchRuntimeFailurePreservesReservationsAndBlocksRemoteCalls(t *testing.T) {
	provider, _, candidate := setupBatchFixture(t)
	ctx := context.Background()
	if err := StartBatchRun(ctx); err != nil && !errors.Is(err, jobqueue.ErrHandlerUnavailable) {
		t.Fatal(err)
	}
	reachSubmittedBatch(t, provider)
	before := provider.requests
	oldKey := config.LLMBATCHAPIKEY
	config.LLMBATCHAPIKEY = ""
	t.Cleanup(func() { config.LLMBATCHAPIKEY = oldKey })
	if InitializeBatchRuntime(ctx) == nil {
		t.Fatal("missing Batch key accepted")
	}
	for _, run := range []func(context.Context) error{ProcessBatchTick, RecoverBatchJobs, StartBatchRun, ResumeBatchCleanup} {
		if err := run(ctx); err == nil {
			t.Fatal("unguarded Batch entrypoint")
		}
	}
	if _, err := ResumeBatchRun(ctx); err == nil {
		t.Fatal("unguarded Batch resume")
	}
	if provider.requests != before {
		t.Fatal("Batch made remote requests while unavailable")
	}
	reserved, err := BatchActiveTarget(db, candidate.MaterialID, "1")
	if err != nil || !reserved {
		t.Fatalf("lost durable reservation: %v", err)
	}
	config.LLMBATCHAPIKEY = oldKey
	if err := BatchRuntimeReady(ctx); err != nil {
		t.Fatal(err)
	}
	var run BatchRun
	if err := db.Where("active_key=1").First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if run.AdmissionEnabled {
		t.Fatal("restart authorized new Batch uploads")
	}
	if err := db.Model(&run).Update("admission_enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := BatchRuntimeReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&run, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !run.AdmissionEnabled {
		t.Fatal("periodic readiness revoked owner authorization")
	}
}

func TestBatchRuntimeRetriesFailedAdmissionReset(t *testing.T) {
	_, _, _ = setupBatchFixture(t)
	ctx := context.Background()
	callbackName := "test:block-admission-reset"
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "assessment_batch_runs" {
			tx.AddError(errors.New("injected admission failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Update().Remove(callbackName) })
	if InitializeBatchRuntime(ctx) == nil {
		t.Fatal("admission reset failure ignored")
	}
	if ProcessBatchTick(ctx) == nil {
		t.Fatal("tick bypassed failed admission reset")
	}
	db.Callback().Update().Remove(callbackName)
	if err := BatchRuntimeReady(ctx); err != nil {
		t.Fatalf("failed reset not retried: %v", err)
	}
}
