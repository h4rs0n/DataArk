package assessment

import (
	"DataArk/config"
	appdatabase "DataArk/database"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"context"
	"errors"
	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"strings"
	"testing"
	"time"
)

// Use a fresh disposable database: this test migrates the actual production schema.
func TestPostgresBatchTransferAndRecovery(t *testing.T) {
	dsn := os.Getenv("DATAARK_BATCH_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_BATCH_POSTGRES_TEST_DSN to a disposable database")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	defer sqlDB.Close()
	var name string
	database.Raw("SELECT current_database()").Scan(&name)
	if !strings.HasPrefix(name, "dataark_v3_verify") {
		t.Fatal("refusing non-verification database")
	}
	if err := appdatabase.RunDatabaseMigrations(database); err != nil {
		t.Fatal(err)
	}
	oldDB := SetDB(database)
	oldDiscovery := discovery.SetDB(database)
	defer SetDB(oldDB)
	defer discovery.SetDB(oldDiscovery)
	oldProvider, oldKey, oldModel := config.LLMBATCHPROVIDER, config.LLMBATCHAPIKEY, config.LLMBATCHMODEL
	oldFactory := batchProviderFactory
	config.LLMBATCHPROVIDER = "bailian"
	config.LLMBATCHAPIKEY = "fixture-key"
	config.LLMBATCHMODEL = "qwen-plus"
	defer func() {
		config.LLMBATCHPROVIDER, config.LLMBATCHAPIKEY, config.LLMBATCHMODEL = oldProvider, oldKey, oldModel
		batchProviderFactory = oldFactory
	}()
	p := &fixtureBatchProvider{status: "completed"}
	batchProviderFactory = func(string) BatchProvider { return p }
	source := discovery.DiscoverySource{Name: "Batch PG", URL: "https://batch-pg.invalid/feed", Type: "rss", Enabled: true}
	if err := database.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if source.ID != 1 {
		t.Fatal("requires fresh database")
	}
	ctx := context.Background()
	handlers := jobqueue.Handlers{AssessArticle: func(context.Context, uint, string) error { t.Error("synchronous provider called"); return nil }, ReservedAssessments: ReservedBatchTargets}
	stop, err := jobqueue.Start(ctx, database, handlers, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c := createAssessmentCandidate(t, "PG", "https://batch-pg.invalid/article", "Queue mechanisms", strings.Repeat("independent queue evidence ", 100), time.Now())
	queue, _ := jobqueue.Default()
	if err := queue.EnqueueAssessArticle(ctx, c.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	// Legacy jobs without extracted content must be visible as skipped, while
	// valid jobs in the same River prefix still reach the provider.
	if err := database.Exec(`INSERT INTO river_job (kind, queue, args, state, scheduled_at, max_attempts)
		VALUES (?, ?, jsonb_build_object('candidate_id', ?::bigint, 'content_version', '0'), 'available', now(), 1)`,
		jobqueue.AssessArticleJobKind, jobqueue.AssessmentQueueName, c.ID).Error; err != nil {
		t.Fatal(err)
	}
	transfer, _ := batchTransfer()
	// Exercise real River uniqueness validation without running the scheduled
	// job. A running tick must also be able to enqueue a distinct successor.
	if err := transfer.EnqueueBatchTick(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	highWater, err := transfer.AssessmentHighWater(ctx)
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	run := BatchRun{AdmissionEnabled: true, ActiveKey: &one, Status: "preparing", BaseURL: config.LLMBATCHBASEURL, Model: "qwen-plus", PolicyVersion: ArticleQualityPolicyVersion, HighWater: highWater, CreatedAt: time.Now()}
	if err := database.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(sqlDB, ".", 44); err == nil {
		t.Fatal("admission migration rolled back an active Batch run")
	}
	if err := appdatabase.RunDatabaseMigrations(database); err != nil {
		t.Fatal(err)
	}
	_, _, err = transfer.TransferAssessments(ctx, highWater, 0, time.Now(), 100, func(tx *gorm.DB, jobs []jobqueue.AssessmentJob) error {
		for _, j := range jobs {
			if err := admitBatchItem(tx, &run, j); err != nil {
				return err
			}
		}
		return errors.New("rollback fixture")
	})
	if err == nil {
		t.Fatal("rollback failed")
	}
	var n int64
	database.Model(&BatchItem{}).Count(&n)
	if n != 0 {
		t.Fatal("item survived rollback")
	}
	database.Table("river_job").Where("kind=? AND state='available'", jobqueue.AssessArticleJobKind).Count(&n)
	if n != 2 {
		t.Fatal("River cancellation survived rollback", n)
	}
	reachSubmittedBatch(t, p)
	database.Table("river_job").Where("kind=? AND state='cancelled'", jobqueue.AssessArticleJobKind).Count(&n)
	if n != 2 {
		t.Fatal("River handoff missing", n)
	}
	var skipped BatchItem
	if err := database.Where("status='skipped'").First(&skipped).Error; err != nil {
		t.Fatal(err)
	}
	if skipped.CandidateID != c.ID || skipped.ContentVersion != 0 || skipped.Error == "" || skipped.ActiveIdentity != nil || skipped.FinishedAt == nil {
		t.Fatalf("legacy job not audited: %#v", skipped)
	}
	var item BatchItem
	database.Where("active_identity IS NOT NULL").First(&item)
	if item.ActiveIdentity == nil {
		t.Fatal("reservation missing")
	}
	if err := queue.EnqueueAssessArticle(ctx, c.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	database.Table("river_job").Where("kind=?", jobqueue.AssessArticleJobKind).Count(&n)
	if n != 2 {
		t.Fatal("in-flight re-enqueued", n)
	}
	stop()
	if err := ResetBatchAdmission(ctx); err != nil {
		t.Fatal(err)
	}
	stop2, err := jobqueue.Start(ctx, database, handlers, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop2()
	if err := ValidateBatchRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	p.output = batchResult(item.CustomID, validBatchAssessment)
	batchStep(t)
	batchStep(t)
	batchStep(t)
	database.First(&item, item.ID)
	if item.Status != "succeeded" || item.AssessmentID == nil {
		t.Fatal(item)
	}
	database.Model(&LLMCall{}).Where("batch_item_id=?", item.ID).Count(&n)
	if n != 1 {
		t.Fatal("usage not persisted", n)
	}
	var schemaCall LLMCall
	if err := database.Where("batch_item_id=?", item.ID).First(&schemaCall).Error; err != nil || schemaCall.ResponseMode != ResponseSchemaMode {
		t.Fatal("Postgres call lost the submitted Schema mode", schemaCall.ResponseMode, err)
	}
	database.First(&run, run.ID)
	if run.ActiveKey != nil {
		t.Fatal("run not completed")
	}
	streamFirst := createAssessmentCandidate(t, "Streaming PG", "https://batch.invalid/stream-first", "First streaming", strings.Repeat("first streaming article evidence ", 100), time.Now())
	if err := queue.EnqueueAssessArticle(ctx, streamFirst.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	verifyBatchStreaming(t, p, streamFirst)
	restartFirst := createAssessmentCandidate(t, "Restart PG", "https://restart.invalid/first", "Restart", strings.Repeat("restart first evidence ", 100), time.Now())
	if err := queue.EnqueueAssessArticle(ctx, restartFirst.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	verifyBatchRestartRemainder(t, p)
	verifyPostgresBatchReset(t)
	verifyCleanupAuthPause(t, p, 401)
	verifyBatchCompatibilityReimport(t, p)
	queuedRun, _, queuedItem, _ := failedReimportFixture(t, p)
	if err := queue.EnqueueAssessArticle(ctx, queuedItem.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	queuedStats, err := ReimportBatchRun(ctx, queuedRun.ID, false)
	if err != nil || queuedStats.Restored != 1 {
		t.Fatal("waiting jobs prevented reuse of existing inference", queuedStats, err)
	}
	runningRun, _, runningItem, _ := failedReimportFixture(t, p)
	if err := queue.EnqueueAssessArticle(ctx, runningItem.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`UPDATE river_job SET state='running',attempted_at=now(),attempt=1 WHERE kind=? AND args @> jsonb_build_object('material_id',?::bigint,'content_version','1')`, jobqueue.AssessArticleJobKind, runningItem.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	runningStats, err := ReimportBatchRun(ctx, runningRun.ID, false)
	if err != nil || runningStats.Restored != 0 || runningStats.Skipped != 1 {
		t.Fatal("reimport overwrote an executing assessment", runningStats, err)
	}
}
