package assessment

import (
	"DataArk/config"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/material"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

type fixtureBatchProvider struct {
	uploads, creates, finds, deletes int
	createError                      error
	uploadError                      error
	output                           string
	remote                           *RemoteBatch
	payload                          []byte
	status                           string
	remoteErrors                     json.RawMessage
	operationErrors                  map[string]error
	requests                         int
}

func (p *fixtureBatchProvider) operation(name string) error {
	p.requests++
	return p.operationErrors[name]
}

func (p *fixtureBatchProvider) Upload(_ context.Context, _ string, data []byte) (BatchFile, error) {
	p.uploads++
	if err := p.operation("upload"); err != nil {
		return BatchFile{}, err
	}
	p.payload = append([]byte(nil), data...)
	if p.uploadError != nil {
		return BatchFile{}, p.uploadError
	}
	return BatchFile{ID: "file-fixture", Status: "processed"}, nil
}
func (p *fixtureBatchProvider) File(context.Context, string) (BatchFile, error) {
	if err := p.operation("file"); err != nil {
		return BatchFile{}, err
	}
	return BatchFile{ID: "file-fixture", Status: "processed"}, nil
}
func (p *fixtureBatchProvider) FindFile(context.Context, string) (*BatchFile, error) {
	if err := p.operation("find_file"); err != nil {
		return nil, err
	}
	if p.uploads > 0 {
		return &BatchFile{ID: "file-fixture", Status: "processed"}, nil
	}
	return nil, nil
}
func (p *fixtureBatchProvider) Create(_ context.Context, file, name string) (RemoteBatch, error) {
	p.creates++
	if err := p.operation("create"); err != nil {
		return RemoteBatch{}, err
	}
	b := RemoteBatch{ID: "remote-fixture", Status: "in_progress", InputFileID: file, Metadata: map[string]string{"ds_name": name}}
	if len(p.remoteErrors) > 0 {
		b.Status = "failed"
		b.Errors = p.remoteErrors
	}
	p.remote = &b
	if p.createError != nil {
		return RemoteBatch{}, p.createError
	}
	return b, nil
}
func (p *fixtureBatchProvider) Retrieve(context.Context, string) (RemoteBatch, error) {
	if err := p.operation("retrieve"); err != nil {
		return RemoteBatch{}, err
	}
	b := *p.remote
	b.Status = p.status
	b.OutputFileID = "output-fixture"
	return b, nil
}
func (p *fixtureBatchProvider) FindBatch(context.Context, string, string) (*RemoteBatch, error) {
	p.finds++
	if err := p.operation("find_batch"); err != nil {
		return nil, err
	}
	return p.remote, nil
}
func (p *fixtureBatchProvider) Download(context.Context, string) (io.ReadCloser, error) {
	if err := p.operation("download"); err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(p.output)), nil
}
func (p *fixtureBatchProvider) Delete(context.Context, string) error {
	p.deletes++
	return p.operation("delete")
}
func setupBatchFixture(t *testing.T) (*fixtureBatchProvider, func(), discovery.DiscoveryCandidate) {
	t.Helper()
	setupAssessmentDB(t)
	if err := db.AutoMigrate(&BatchRun{}, &Batch{}, &BatchItem{}); err != nil {
		t.Fatal(err)
	}
	oldProvider, oldKey, oldModel := config.LLMBATCHPROVIDER, config.LLMBATCHAPIKEY, config.LLMBATCHMODEL
	oldFactory := batchProviderFactory
	config.LLMBATCHPROVIDER = "bailian"
	config.LLMBATCHAPIKEY = "fixture-secret"
	config.LLMBATCHMODEL = "qwen-plus"
	p := &fixtureBatchProvider{status: "completed"}
	batchProviderFactory = func(string) BatchProvider { return p }
	t.Cleanup(func() {
		config.LLMBATCHPROVIDER, config.LLMBATCHAPIKEY, config.LLMBATCHMODEL = oldProvider, oldKey, oldModel
		batchProviderFactory = oldFactory
	})
	// Manual steps make restart and timeout transitions deterministic.
	stop, err := jobqueue.Start(context.Background(), db, jobqueue.Handlers{AssessArticle: func(context.Context, uint, string) error { t.Error("synchronous assessment called"); return nil }, ReservedAssessments: ReservedBatchTargets}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	c := createAssessmentCandidate(t, "Batch fixture", "https://batch.invalid/article", "Batch article", strings.Repeat("substantive article evidence ", 100), time.Now())
	q, _ := jobqueue.Default()
	if err := q.EnqueueAssessArticle(context.Background(), c.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	return p, stop, c
}
func batchStep(t *testing.T) {
	t.Helper()
	var run BatchRun
	if err := db.Where("active_key=1").First(&run).Error; err != nil {
		t.Fatal(err)
	}
	q, err := batchTransfer()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Batch{}).Where("run_id=?", run.ID).Update("next_check_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := processBatchRunStep(context.Background(), &run, q); err != nil {
		t.Fatal(err)
	}
}
func reachSubmittedBatch(t *testing.T, p *fixtureBatchProvider) {
	t.Helper()
	for i := 0; i < 8 && p.creates == 0; i++ {
		batchStep(t)
	}
	if p.creates != 1 {
		t.Fatal("batch not submitted", p.creates)
	}
}
func batchResult(id, content string) string {
	return fmt.Sprintf(`{"custom_id":%q,"response":{"status_code":200,"body":{"choices":[{"finish_reason":"stop","message":{"content":%q}}],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}},"error":null}`, id, content) + "\n"
}

const validBatchAssessment = `{"qualityScore":85,"depthScore":70,"evergreenScore":80,"reasons":["论据充分","范围有限"],"summary":"解释队列运行与权衡。","keywords":["队列","调度","评估"]}`

func TestBatchStreamsFullBatchesBeforePrefixEndsAndFlushesTail(t *testing.T) {
	p, _, first := setupBatchFixture(t)
	verifyBatchStreaming(t, p, first)
}

// Shared with the real PostgreSQL/River test: the first admission page contains
// a full batch, with a final article left in the original queue prefix.
func verifyBatchStreaming(t *testing.T, p *fixtureBatchProvider, first discovery.DiscoveryCandidate) {
	t.Helper()
	oldSize, oldConcurrency := config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY
	config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY = 2, 1
	t.Cleanup(func() { config.LLMBATCHSIZE, config.LLMBATCHCONCURRENCY = oldSize, oldConcurrency })
	ctx := context.Background()
	q, _ := jobqueue.Default()
	second := createAssessmentCandidate(t, "Streaming", "https://batch.invalid/stream-second", "Second", strings.Repeat("second article evidence ", 100), time.Now())
	if err := q.EnqueueAssessArticle(ctx, second.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 4; n++ {
		if err := q.EnqueueAssessArticle(ctx, uint(900000+n), "0"); err != nil {
			t.Fatal(err)
		}
	}
	tail := createAssessmentCandidate(t, "Streaming", "https://batch.invalid/stream-tail", "Tail", strings.Repeat("tail article evidence ", 100), time.Now())
	if err := q.EnqueueAssessArticle(ctx, tail.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	_ = StartBatchRun(ctx)
	initialCreates := p.creates
	for n := 0; n < 8 && p.creates == initialCreates; n++ {
		batchStep(t)
	}
	if p.creates != initialCreates+1 {
		t.Fatal("first full batch was not submitted")
	}
	var run BatchRun
	if err := db.Where("active_key=1").First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != "preparing" || run.Cursor >= run.HighWater {
		t.Fatalf("submission waited for the entire prefix: %#v", run)
	}
	var items []BatchItem
	if err := db.Where("run_id=? AND status='processing'", run.ID).Order("id").Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].MaterialID != first.MaterialID || items[1].MaterialID != second.MaterialID {
		t.Fatalf("first batch membership: %#v", items)
	}
	// A future poll and the concurrency limit prevent both further admission
	// and premature run completion while the provider is working.
	transfer, err := batchTransfer()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Batch{}).Where("run_id=?", run.ID).Update("next_check_at", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := processBatchRunStep(ctx, &run, transfer); err != nil {
		t.Fatal(err)
	}
	var unchanged BatchRun
	if err := db.First(&unchanged, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Cursor != run.Cursor || unchanged.ActiveKey == nil {
		t.Fatal("busy batch did not apply backpressure", unchanged)
	}
	p.output = batchResult(items[0].CustomID, validBatchAssessment) + batchResult(items[1].CustomID, validBatchAssessment)
	batchStep(t) // retrieve
	batchStep(t) // import
	for n := 0; n < 16 && p.creates < initialCreates+2; n++ {
		batchStep(t)
	}
	if p.creates != initialCreates+2 {
		t.Fatal("final partial batch was not submitted")
	}
	var tailItem BatchItem
	if err := db.Where("run_id=? AND material_id=?", run.ID, tail.MaterialID).First(&tailItem).Error; err != nil {
		t.Fatal(err)
	}
	p.output = batchResult(tailItem.CustomID, validBatchAssessment)
	batchStep(t)
	batchStep(t)
	batchStep(t)
	if err := db.First(&run, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := BatchRunCounts(ctx, &run); err != nil {
		t.Fatal(err)
	}
	if run.ActiveKey != nil || run.Status != "completed" || run.Counts.Succeeded != 3 || run.Counts.Skipped != 4 {
		t.Fatalf("streaming run did not finish: %#v", run)
	}
}

func TestBatchAdmissionAuditsUnusableJobsAndSubmitsValidContent(t *testing.T) {
	p, _, c := setupBatchFixture(t)
	_ = StartBatchRun(context.Background()) // fixture intentionally has no tick handler
	var run BatchRun
	if err := db.Where("active_key=1").First(&run).Error; err != nil {
		t.Fatal(err)
	}
	jobs := []jobqueue.AssessmentJob{
		{ID: "legacy-zero", CandidateID: c.ID, ContentVersion: "0"},
		{ID: "legacy-missing", CandidateID: c.ID, ContentVersion: "99"},
		{ID: "material-missing", MaterialID: c.MaterialID, ContentVersion: "99"},
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, j := range jobs {
			if err := admitBatchItem(tx, &run, j); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var skipped []BatchItem
	if err := db.Where("run_id=? AND status='skipped'", run.ID).Order("id").Find(&skipped).Error; err != nil {
		t.Fatal(err)
	}
	if len(skipped) != len(jobs) {
		t.Fatalf("skipped items = %d, want %d", len(skipped), len(jobs))
	}
	for i, item := range skipped {
		if item.JobID != jobs[i].ID || item.Error == "" || item.FinishedAt == nil || item.ActiveIdentity != nil || item.BatchID != nil {
			t.Fatalf("invalid skipped item: %#v", item)
		}
	}
	reachSubmittedBatch(t, p)
	var pending []BatchItem
	if err := db.Where("run_id=? AND active_identity IS NOT NULL", run.ID).Find(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].MaterialID != c.MaterialID || pending[0].ContentVersion != 1 {
		t.Fatalf("valid content not submitted: %#v", pending)
	}
	if strings.Count(strings.TrimSpace(string(p.payload)), "\n") != 0 {
		t.Fatal("unusable jobs included in provider input")
	}
	p.output = batchResult(pending[0].CustomID, validBatchAssessment)
	batchStep(t)
	batchStep(t)
	batchStep(t)
	if err := db.First(&run, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := BatchRunCounts(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || run.Counts.Skipped != 3 || run.Counts.Succeeded != 1 {
		t.Fatalf("run did not complete with visible skipped counts: %#v", run)
	}
}

func TestBatchRunFixedMembershipRecoveryAndIdempotentImport(t *testing.T) {
	p, stop, c := setupBatchFixture(t)
	ctx := context.Background()
	if err := StartBatchRun(ctx); err == nil { // no tick handler intentionally: durable registration survives failed notification
		t.Fatal("expected scheduling failure")
	}
	q, _ := jobqueue.Default()
	late := createAssessmentCandidate(t, "fixture", "https://batch.invalid/late", "Late", strings.Repeat("late evidence ", 100), time.Now())
	if err := q.EnqueueAssessArticle(ctx, late.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	reachSubmittedBatch(t, p)
	var items []BatchItem
	db.Find(&items)
	if len(items) != 1 || items[0].MaterialID != c.MaterialID {
		t.Fatal("membership changed", items)
	}
	var request struct {
		Body map[string]any `json:"body"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(p.payload))), &request); err != nil {
		t.Fatal(err)
	}
	if request.Body["enable_thinking"] != false || request.Body["max_tokens"] != nil || request.Body["min_p"] != nil || request.Body["chat_template_kwargs"] != nil {
		t.Fatal(request.Body)
	}
	reserved, err := ReservedBatchTargets(ctx, []jobqueue.AssessmentTarget{{MaterialID: c.MaterialID, ContentVersion: "1"}})
	if err != nil || len(reserved) != 1 {
		t.Fatal(reserved, err)
	}
	if err := q.EnqueueAssessArticle(ctx, c.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := q.(interface {
		AssessmentHighWater(context.Context) (int64, error)
	}).AssessmentHighWater(ctx)
	_ = snapshot
	stop() // recreate the queue as during a process restart
	stop2, err := jobqueue.Start(ctx, db, jobqueue.Handlers{AssessArticle: func(context.Context, uint, string) error { return nil }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop2()
	config.LLMBATCHPROVIDER = ""
	if err := ValidateBatchRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	config.LLMBATCHAPIKEY = ""
	if ValidateBatchRecovery(ctx) == nil {
		t.Fatal("missing recovery credential accepted")
	}
	config.LLMBATCHAPIKEY = "fixture-secret"
	p.output = batchResult(items[0].CustomID, validBatchAssessment)
	batchStep(t)
	batchStep(t)
	var row BatchItem
	db.First(&row, items[0].ID)
	if row.Status != "succeeded" || row.ActiveIdentity != nil {
		t.Fatal(row)
	}
	var batch Batch
	db.First(&batch)
	var run BatchRun
	db.First(&run)
	if err := importBatch(ctx, &run, &batch, p); err != nil {
		t.Fatal(err)
	}
	var calls int64
	db.Model(&LLMCall{}).Where("batch_item_id=?", row.ID).Count(&calls)
	if calls != 1 {
		t.Fatal("usage duplicated", calls)
	}
	var state material.ArticleState
	db.First(&state, "material_id=?", c.MaterialID)
	if state.CurrentAssessmentID == nil || state.AssessmentState != "ready" {
		t.Fatal(state)
	}
	batchStep(t)
	db.First(&run, run.ID)
	if run.ActiveKey != nil || run.Status != "completed" {
		t.Fatal(run)
	}
	if p.creates != 1 {
		t.Fatal("restart resubmitted")
	}
}
func TestBatchUnknownSubmissionIsReconciledWithoutDuplicate(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	p.createError = errors.New("lost response")
	_ = StartBatchRun(context.Background())
	reachSubmittedBatch(t, p)
	var batch Batch
	db.First(&batch)
	if batch.Phase != "submission_unknown" {
		t.Fatal(batch.Phase)
	}
	batchStep(t)
	db.First(&batch, batch.ID)
	if batch.Phase != "submitted" || p.creates != 1 || p.finds != 1 {
		t.Fatal(batch, p.creates, p.finds)
	}
}
func TestBatchImportRejectsInvalidAndStaleResults(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			p, _, c := setupBatchFixture(t)
			_ = StartBatchRun(context.Background())
			reachSubmittedBatch(t, p)
			var item BatchItem
			db.First(&item)
			content := `{"qualityScore":999}`
			if stale {
				content = validBatchAssessment
				if err := db.Model(&material.ArticleState{}).Where("material_id=?", c.MaterialID).Update("content_version", 2).Error; err != nil {
					t.Fatal(err)
				}
			}
			p.output = batchResult(item.CustomID, content)
			batchStep(t)
			batchStep(t)
			db.First(&item, item.ID)
			want := "failed"
			if stale {
				want = "skipped"
			}
			if item.Status != want {
				t.Fatal(item)
			}
			var state material.ArticleState
			db.First(&state, "material_id=?", c.MaterialID)
			if state.CurrentAssessmentID != nil {
				t.Fatal("invalid or stale activated")
			}
		})
	}
}

func TestBatchPartialFailureRequiresManualRetry(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	ctx := context.Background()
	second := createAssessmentCandidate(t, "Invalid batch", "https://batch.invalid/invalid", "Invalid", strings.Repeat("separate evidence ", 100), time.Now())
	q, _ := jobqueue.Default()
	if err := q.EnqueueAssessArticle(ctx, second.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	_ = StartBatchRun(ctx)
	reachSubmittedBatch(t, p)
	var items []BatchItem
	if err := db.Order("id").Find(&items).Error; err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	p.output = batchResult(items[1].CustomID, `{"qualityScore":999}`) + "\n" + batchResult(items[0].CustomID, validBatchAssessment)
	batchStep(t)
	batchStep(t)
	batchStep(t)
	var run BatchRun
	db.First(&run)
	if err := BatchRunCounts(ctx, &run); err != nil {
		t.Fatal(err)
	}
	if run.Active || run.Counts.Succeeded != 1 || run.Counts.Failed != 1 {
		t.Fatal(run)
	}
	if err := RecoverBatchJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if p.creates != 1 {
		t.Fatal("automatic failure resubmission")
	}
	options := ArticleAssessmentBatchOptions{Limit: 250}
	result, err := PrepareArticleAssessmentBackfill(ctx, ConfiguredArticleAssessor(), q, options)
	if err != nil || result.Enqueued != 0 {
		t.Fatal(result, err)
	}
	options.RetryFailures = true
	result, err = PrepareArticleAssessmentBackfill(ctx, ConfiguredArticleAssessor(), q, options)
	if err != nil || result.Enqueued != 1 {
		t.Fatal(result, err)
	}
	if p.creates != 1 {
		t.Fatal("backfill called provider")
	}
	_ = StartBatchRun(ctx)
	// Advance the new run explicitly, since the provider already has a prior submission.
	for n := 0; n < 8 && p.creates < 2; n++ {
		batchStep(t)
	}
	if p.creates != 2 {
		t.Fatal("manual retry did not submit", p.creates)
	}
}

func TestBatchCleanupOwnFilesAndRunLease(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	ctx := context.Background()
	_ = StartBatchRun(ctx)
	var run BatchRun
	db.First(&run)
	// A live lease prevents concurrent workers from preparing the same prefix.
	future := time.Now().Add(time.Hour)
	if err := db.Model(&run).Update("lease_until", future).Error; err != nil {
		t.Fatal(err)
	}
	_ = ProcessBatchTick(ctx)
	var count int64
	db.Model(&BatchItem{}).Count(&count)
	if count != 0 {
		t.Fatal("live lease ignored")
	}
	if err := db.Model(&run).Update("lease_until", nil).Error; err != nil {
		t.Fatal(err)
	}
	_ = ProcessBatchTick(ctx)
	db.First(&run, run.ID)
	if run.LeaseUntil != nil {
		t.Fatal("lease not released")
	}
	reachSubmittedBatch(t, p)
	var item BatchItem
	db.First(&item)
	p.output = batchResult(item.CustomID, validBatchAssessment)
	batchStep(t)
	batchStep(t)
	batchStep(t)
	if err := cleanupBatchFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if p.deletes != 0 {
		t.Fatal("deleted before retention elapsed")
	}
	if err := db.Model(&Batch{}).Where("phase='done'").Update("finished_at", time.Now().Add(-8*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := cleanupBatchFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if p.deletes != 2 {
		t.Fatal("own input/output not removed", p.deletes)
	}
	if err := cleanupBatchFiles(ctx); err != nil {
		t.Fatal(err)
	}
	if p.deletes != 2 {
		t.Fatal("cleanup repeated")
	}
}

func TestBatchNeverLabelsNewPromptWithAnOldPolicy(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	_ = StartBatchRun(context.Background())
	if err := db.Model(&BatchRun{}).Where("active_key=1").Update("policy_version", "old-policy").Error; err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		batchStep(t)
	}
	var item BatchItem
	db.First(&item)
	if item.Status != "failed" || p.uploads != 0 || p.creates != 0 {
		t.Fatal("old policy submitted with current prompt", item, p.uploads, p.creates)
	}
}
