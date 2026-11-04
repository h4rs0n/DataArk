package assessment

import (
	"DataArk/articlevalue"
	"DataArk/config"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/material"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var batchProviderFactory = func(baseURL string) BatchProvider {
	timeout, _ := time.ParseDuration(config.LLMBATCHREQUESTTIMEOUT)
	return BailianBatchProvider{BaseURL: baseURL, APIKey: config.LLMBATCHAPIKEY, Client: &http.Client{Timeout: timeout}}
}

func batchTransfer() (jobqueue.AssessmentTransfer, error) {
	q, ok := jobqueue.Default()
	if !ok {
		return nil, fmt.Errorf("assessment queue unavailable")
	}
	t, ok := q.(jobqueue.AssessmentTransfer)
	if !ok {
		return nil, fmt.Errorf("assessment queue cannot transfer Batch jobs")
	}
	return t, nil
}
func BatchActiveTarget(database *gorm.DB, id uint, version string) (bool, error) {
	if !database.Migrator().HasTable(&BatchItem{}) {
		return false, nil
	}
	var count int64
	err := database.Model(&BatchItem{}).Where("active_identity = ?", fmt.Sprintf("%d:%s", id, version)).Count(&count).Error
	return count > 0, err
}
func ValidateBatchRecovery(ctx context.Context) error {
	if db == nil || !db.Migrator().HasTable(&BatchRun{}) {
		return nil
	}
	var runs []BatchRun
	if err := db.WithContext(ctx).Where("active_key=1").Find(&runs).Error; err != nil {
		return err
	}
	for _, r := range runs {
		if strings.TrimRight(config.LLMBATCHBASEURL, "/") != r.BaseURL || strings.TrimSpace(config.LLMBATCHAPIKEY) == "" {
			return fmt.Errorf("unfinished Batch recovery requires the original -llm-batch-base-url and -llm-batch-api-key")
		}
	}
	return nil
}

// ResetBatchAdmission runs once before workers start, never in periodic recovery.
// Uploads already started retain permission to finish; new work needs owner approval.
func ResetBatchAdmission(ctx context.Context) error {
	if db == nil {
		return fmt.Errorf("assessment database unavailable")
	}
	return db.WithContext(ctx).Model(&BatchRun{}).Where("active_key=1").Update("admission_enabled", false).Error
}

// runnableBatches excludes uploads that have not started after a restart.
func runnableBatches(ctx context.Context, run *BatchRun) *gorm.DB {
	q := db.WithContext(ctx).Model(&Batch{}).Where("run_id=? AND phase<>'done'", run.ID)
	if !run.AdmissionEnabled {
		q = q.Where("phase<>'prepared'")
	}
	return q
}

func StartBatchRun(ctx context.Context) error {
	if !config.BatchEnabled() {
		return fmt.Errorf("Batch is not configured")
	}
	cleanup, err := pausedBatchCleanup(ctx)
	if err != nil {
		return err
	}
	if cleanup != nil {
		return fmt.Errorf("Batch file cleanup is paused; manually resume cleanup before starting inference")
	}
	transfer, err := batchTransfer()
	if err != nil {
		return err
	}
	highWater, err := transfer.AssessmentHighWater(ctx)
	if err != nil {
		return err
	}
	paused := false
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(7845041)").Error; err != nil {
				return err
			}
		}
		var active BatchRun
		if err := tx.Where("active_key=1").First(&active).Error; err == nil {
			paused = active.Status == "paused"
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		one := 1
		run := BatchRun{AdmissionEnabled: true, ActiveKey: &one, Status: "preparing", BaseURL: strings.TrimRight(config.LLMBATCHBASEURL, "/"), Model: config.LLMBATCHMODEL, PolicyVersion: articlevalue.PolicyVersion, HighWater: highWater, CreatedAt: time.Now().UTC()}
		return tx.Create(&run).Error
	})
	if err != nil {
		return err
	}
	if paused {
		return nil
	}
	return transfer.EnqueueBatchTick(context.Background(), time.Now())
}

// RecoverBatchJobs is also used on a timer so a failed scheduling operation
// cannot strand durable runs. It never authorizes a new run.
func RecoverBatchJobs(ctx context.Context) error {
	if db == nil || !db.Migrator().HasTable(&BatchRun{}) {
		return nil
	}
	var count int64
	err := db.WithContext(ctx).Model(&BatchRun{}).Where(`
 NOT EXISTS (SELECT 1 FROM assessment_batch_runs WHERE resume_status='cleanup' AND paused_at IS NOT NULL)
 AND (
   (active_key=1 AND status<>'paused' AND (
     admission_enabled=true
     OR (status='processing' AND NOT EXISTS (
       SELECT 1 FROM assessment_batch_items WHERE run_id=assessment_batch_runs.id AND active_identity IS NOT NULL
     ))
     OR EXISTS (
       SELECT 1 FROM assessment_batches WHERE run_id=assessment_batch_runs.id AND phase NOT IN ('prepared','done')
     )
   ))
   OR (status<>'paused' AND id IN (
     SELECT run_id FROM assessment_batches WHERE cleaned_at IS NULL AND finished_at <= ?
   ))
 )`, time.Now().Add(-7*24*time.Hour)).Count(&count).Error
	if err != nil || count == 0 {
		return err
	}
	transfer, err := batchTransfer()
	if err != nil {
		return err
	}
	return transfer.EnqueueBatchTick(ctx, time.Now())
}
func ProcessBatchTick(ctx context.Context) error {
	if db == nil {
		return fmt.Errorf("assessment database unavailable")
	}
	cleanup, err := pausedBatchCleanup(ctx)
	if err != nil || cleanup != nil {
		return err
	}
	transfer, err := batchTransfer()
	if err != nil {
		return err
	}
	var run BatchRun
	err = db.WithContext(ctx).Where("active_key=1").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return cleanupBatchFiles(ctx)
	}
	if err != nil {
		return err
	}
	if run.Status == "paused" {
		return nil
	}
	now := time.Now().UTC()
	lease := now.Add(6 * time.Minute).Truncate(time.Microsecond)
	acquired := db.WithContext(ctx).Model(&BatchRun{}).Where("id=? AND (lease_until IS NULL OR lease_until<?)", run.ID, now).Update("lease_until", lease)
	if acquired.Error != nil {
		return acquired.Error
	}
	if acquired.RowsAffected == 0 {
		return transfer.EnqueueBatchTick(ctx, now.Add(time.Minute))
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.WithContext(releaseCtx).Model(&BatchRun{}).Where("id=? AND lease_until=?", run.ID, lease).Update("lease_until", nil).Error
	}()
	err = processBatchRunStep(ctx, &run, transfer)
	if run.Status == "paused" {
		return err
	}
	delay := time.Second
	if err != nil {
		delay, _ = time.ParseDuration(config.LLMBATCHPOLLINTERVAL)
	}
	var next Batch
	if e := runnableBatches(ctx, &run).Omit("input_jsonl").Order("next_check_at,id").Limit(1).Find(&next).Error; e != nil {
		return errors.Join(err, e)
	} else if next.ID == 0 && !run.AdmissionEnabled && err == nil {
		if e := finishBatchRunIfDone(ctx, &run); e != nil {
			return e
		}
		return cleanupBatchFiles(ctx)
	} else if next.ID != 0 && next.NextCheckAt.After(now) {
		delay = time.Until(next.NextCheckAt)
	}
	var unbatched, outstanding int64
	if err == nil {
		if e := db.WithContext(ctx).Model(&BatchItem{}).Where("run_id=? AND status='pending' AND batch_id IS NULL", run.ID).Count(&unbatched).Error; e != nil {
			return e
		}
		if e := db.WithContext(ctx).Model(&Batch{}).Where("run_id=? AND phase<>'done'", run.ID).Count(&outstanding).Error; e != nil {
			return e
		}
		if run.AdmissionEnabled && (unbatched > 0 || run.Status == "preparing") && outstanding < int64(config.LLMBATCHCONCURRENCY) {
			delay = time.Second
		}
	}
	if delay < time.Second {
		delay = time.Second
	}
	scheduleErr := transfer.EnqueueBatchTick(ctx, time.Now().Add(delay))
	return errors.Join(err, scheduleErr)
}
func processBatchRunStep(ctx context.Context, run *BatchRun, transfer jobqueue.AssessmentTransfer) error {
	if run.Status == "paused" || run.Status == "rolled_back" {
		return nil
	}
	// Advance durable uploads and results even while queue admission is ongoing.
	var batch Batch
	query := runnableBatches(ctx, run).Where("next_check_at<=?", time.Now()).Order("id").Limit(1).Find(&batch)
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected > 0 {
		return advanceBatch(ctx, run, &batch)
	}
	var outstanding int64
	if err := db.WithContext(ctx).Model(&Batch{}).Where("run_id=? AND phase<>'done'", run.ID).Count(&outstanding).Error; err != nil {
		return err
	}
	// Backpressure keeps the remaining prefix in River while all slots are busy.
	if outstanding >= int64(config.LLMBATCHCONCURRENCY) {
		return nil
	}
	var unbatched int64
	if err := db.WithContext(ctx).Model(&BatchItem{}).Where("run_id=? AND status='pending' AND batch_id IS NULL", run.ID).Count(&unbatched).Error; err != nil {
		return err
	}
	if run.AdmissionEnabled && run.Status == "preparing" && unbatched < int64(config.LLMBATCHSIZE) {
		cursor, done, err := transfer.TransferAssessments(jobqueue.WithBatchRun(ctx, run.ID), run.HighWater, run.Cursor, run.CreatedAt, config.LLMBATCHSIZE-int(unbatched), func(tx *gorm.DB, jobs []jobqueue.AssessmentJob) error {
			for _, j := range jobs {
				if err := admitBatchItem(tx, run, j); err != nil {
					return err
				}
			}
			if len(jobs) > 0 {
				return tx.Model(&BatchRun{}).Where("id=?", run.ID).Update("cursor", jobs[len(jobs)-1].Cursor).Error
			}
			return nil
		})
		if err != nil {
			return err
		}
		run.Cursor = cursor
		if done {
			if err := db.WithContext(ctx).Model(&BatchRun{}).Where("id=?", run.ID).Update("status", "processing").Error; err != nil {
				return err
			}
			run.Status = "processing"
		}
		if err := db.WithContext(ctx).Model(&BatchItem{}).Where("run_id=? AND status='pending' AND batch_id IS NULL", run.ID).Count(&unbatched).Error; err != nil {
			return err
		}
	}
	// Submit a full batch immediately; flush the final partial batch once the
	// fixed prefix is exhausted. Existing preparing runs can use admitted items.
	if run.AdmissionEnabled && unbatched > 0 && (unbatched >= int64(config.LLMBATCHSIZE) || run.Status != "preparing") {
		created, err := buildBatch(ctx, run)
		if err != nil || created {
			return err
		}
	}
	if run.Status == "preparing" {
		return nil
	}
	return finishBatchRunIfDone(ctx, run)
}

func finishBatchRunIfDone(ctx context.Context, run *BatchRun) error {
	if run.Status != "processing" {
		return nil
	}
	var pending, outstanding int64
	if err := db.WithContext(ctx).Model(&BatchItem{}).Where("run_id=? AND active_identity IS NOT NULL", run.ID).Count(&pending).Error; err != nil {
		return err
	}
	if err := db.WithContext(ctx).Model(&Batch{}).Where("run_id=? AND phase<>'done'", run.ID).Count(&outstanding).Error; err != nil {
		return err
	}
	if pending == 0 && outstanding == 0 {
		now := time.Now().UTC()
		return db.WithContext(ctx).Model(&BatchRun{}).Where("id=? AND status='processing'", run.ID).Updates(map[string]any{"status": "completed", "active_key": nil, "finished_at": now}).Error
	}
	return nil
}
func admitBatchItem(tx *gorm.DB, run *BatchRun, j jobqueue.AssessmentJob) error {
	id := j.MaterialID
	version, err := strconv.ParseUint(j.ContentVersion, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid assessment content version")
	}
	// Version zero has no extracted content. Keep an audit entry instead of
	// silently cancelling a legacy queue job without any Batch item.
	if version == 0 {
		return skipBatchAdmission(tx, run, j, id, uint(version), "assessment job has no extracted content version")
	}
	if id == 0 {
		var ref material.CandidateVersion
		query := tx.Where("candidate_id=? AND content_version=?", j.CandidateID, version).Limit(1).Find(&ref)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected == 0 {
			return skipBatchAdmission(tx, run, j, id, uint(version), "legacy candidate content version not found")
		}
		id = ref.MaterialID
	}
	identity := fmt.Sprintf("%d:%d", id, version)
	var exists int64
	if err := tx.Model(&BatchItem{}).Where("active_identity=?", identity).Count(&exists).Error; err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	var v material.Version
	query := tx.Where("material_id=? AND version=?", id, version).Limit(1).Find(&v)
	if query.Error != nil {
		return query.Error
	}
	if query.RowsAffected == 0 {
		return skipBatchAdmission(tx, run, j, id, uint(version), "material content version not found")
	}
	item := BatchItem{RunID: run.ID, JobID: j.ID, MaterialID: id, VersionID: v.ID, ContentVersion: uint(version), CustomID: uuid.NewString(), ActiveIdentity: &identity, Status: "pending", CreatedAt: time.Now().UTC()}
	var c discovery.DiscoveryCandidate
	if err := discovery.Candidates(tx).Where("material_id=?", id).Order("id").Limit(1).Find(&c).Error; err != nil {
		return err
	}
	item.CandidateID = c.ID
	return tx.Create(&item).Error
}

func skipBatchAdmission(tx *gorm.DB, run *BatchRun, j jobqueue.AssessmentJob, materialID, version uint, reason string) error {
	now := time.Now().UTC()
	return tx.Create(&BatchItem{
		RunID: run.ID, JobID: j.ID, MaterialID: materialID, CandidateID: j.CandidateID,
		ContentVersion: version, CustomID: uuid.NewString(), Status: "skipped",
		Error: reason, CreatedAt: now, FinishedAt: &now,
	}).Error
}
func batchCandidate(tx *gorm.DB, item BatchItem) (discovery.DiscoveryCandidate, bool, error) {
	var state material.ArticleState
	if err := tx.First(&state, "material_id=?", item.MaterialID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return discovery.DiscoveryCandidate{}, false, nil
		}
		return discovery.DiscoveryCandidate{}, false, err
	}
	if state.ContentVersion != item.ContentVersion {
		return discovery.DiscoveryCandidate{}, false, nil
	}
	allowed, err := discovery.DedupeAssessmentAllowed(tx, item.MaterialID)
	if err != nil || !allowed {
		return discovery.DiscoveryCandidate{}, false, err
	}
	var candidate discovery.DiscoveryCandidate
	query := discovery.Candidates(tx).Where("material_id=?", item.MaterialID)
	result := query.Order("id").Limit(1).Find(&candidate)
	if result.Error != nil {
		return candidate, false, result.Error
	}
	if result.RowsAffected == 0 {
		return candidate, false, nil
	}
	if candidate.ProcessingState != discovery.DiscoveryProcessingReady || candidate.DedupeState != discovery.DiscoveryDedupeReady {
		return candidate, false, nil
	}
	return candidate, true, nil
}
func itemRequest(item BatchItem, run *BatchRun) ([]byte, int, error) {
	if run.PolicyVersion != articlevalue.PolicyVersion {
		return nil, 0, fmt.Errorf("assessment policy changed before Batch preparation; retry manually")
	}
	var v material.Version
	if err := db.First(&v, item.VersionID).Error; err != nil {
		return nil, 0, err
	}
	var body material.Representation
	if err := db.Where("version_id=? AND kind='text' AND role='body'", item.VersionID).First(&body).Error; err != nil {
		return nil, 0, err
	}
	evidence, err := articlevalue.BuildEvidence(v.Title, body.Text)
	if err != nil {
		return nil, 0, err
	}
	payload := map[string]any{"model": run.Model, "messages": articleAssessmentMessages(evidence), "response_format": articleAssessmentResponseFormat(), "stream": false, "temperature": 0.7, "top_p": 0.8, "presence_penalty": 1.5}
	if strings.Contains(strings.ToLower(run.Model), "qwen") {
		payload["enable_thinking"] = false
		payload["top_k"] = 20
		payload["repetition_penalty"] = 1.0
	}
	line, err := json.Marshal(map[string]any{"custom_id": item.CustomID, "method": "POST", "url": "/v1/chat/completions", "body": payload})
	if err != nil {
		return nil, 0, err
	}
	if len(line) > 1<<20 {
		return nil, 0, fmt.Errorf("Batch input line exceeds 1 MiB")
	}
	return append(line, '\n'), evidence.EstimatedTokens, nil
}
func buildBatch(ctx context.Context, run *BatchRun) (bool, error) {
	var items []BatchItem
	if err := db.WithContext(ctx).Where("run_id=? AND status='pending' AND batch_id IS NULL", run.ID).Order("id").Limit(config.LLMBATCHSIZE).Find(&items).Error; err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, nil
	}
	var data bytes.Buffer
	selected := []BatchItem{}
	for _, item := range items {
		c, ok, err := batchCandidate(db.WithContext(ctx), item)
		if err != nil {
			return false, err
		}
		if !ok {
			if err := finishBatchItem(ctx, run, item, "skipped", nil, "article version or eligibility changed", nil); err != nil {
				return false, err
			}
			continue
		}
		var row ArticleAssessment
		found := db.Where("material_id=? AND content_version=? AND assessor=? AND assessor_version=? AND policy_version=?", item.MaterialID, item.ContentVersion, "bailian_batch", run.Model, run.PolicyVersion).First(&row)
		if found.Error == nil {
			r := assessmentResultFromRow(row)
			if err := finishBatchItem(ctx, run, item, "reused", &r, "", nil); err != nil {
				return false, err
			}
			continue
		}
		if !errors.Is(found.Error, gorm.ErrRecordNotFound) {
			return false, found.Error
		}
		_ = c
		line, tokens, err := itemRequest(item, run)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, err
			}
			if err := finishBatchItem(ctx, run, item, "failed", nil, err.Error(), nil); err != nil {
				return false, err
			}
			continue
		}
		if data.Len()+len(line) > 50<<20 {
			break
		}
		data.Write(line)
		item.EvidenceTokens = tokens
		selected = append(selected, item)
	}
	if len(selected) == 0 {
		return true, nil
	}
	sum := sha256.Sum256(data.Bytes())
	now := time.Now().UTC()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		b := Batch{RunID: run.ID, UUID: uuid.NewString(), Model: run.Model, ResponseMode: ResponseSchemaMode, Phase: "prepared", InputJSONL: data.Bytes(), Digest: hex.EncodeToString(sum[:]), FileBytes: int64(data.Len()), Total: len(selected), NextCheckAt: now, CreatedAt: now}
		if err := tx.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Create(&b).Error; err != nil {
			return err
		}
		for _, i := range selected {
			if err := tx.Model(&BatchItem{}).Where("id=? AND status='pending'", i.ID).Updates(map[string]any{"batch_id": b.ID, "evidence_tokens": i.EvidenceTokens, "status": "processing"}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return true, err
}
func batchInput(b Batch, run *BatchRun) ([]byte, error) {
	sum := sha256.Sum256(b.InputJSONL)
	if len(b.InputJSONL) == 0 || hex.EncodeToString(sum[:]) != b.Digest {
		return nil, fmt.Errorf("Batch persisted input digest changed")
	}
	return b.InputJSONL, nil
}
func deferBatch(b *Batch, phase string, err error) error {
	if isBatchPauseError(err) {
		return err
	}
	interval, _ := time.ParseDuration(config.LLMBATCHPOLLINTERVAL)
	failures := b.FailureCount
	if err != nil {
		failures++
		for n := 1; n < failures && interval < 15*time.Minute; n++ {
			interval *= 2
		}
		if interval > 15*time.Minute {
			interval = 15 * time.Minute
		}
	} else {
		failures = 0
	}
	message := ""
	if err != nil {
		message = safeBatchError(err)
	}
	return db.Model(b).Updates(map[string]any{"phase": phase, "error": message, "failure_count": failures, "next_check_at": time.Now().Add(interval)}).Error
}
func safeBatchError(err error) string { return safeBatchMessage(err.Error()) }

func advanceBatch(ctx context.Context, run *BatchRun, b *Batch) (stepErr error) {
	if run.Status == "paused" || (!run.AdmissionEnabled && b.Phase == "prepared") {
		return nil
	}
	resumePhase := b.Phase
	defer func() {
		if isBatchPauseError(stepErr) {
			phase := resumePhase
			if b.RemoteID != "" && (b.Phase == "submitted" || b.Phase == "importing") {
				phase = b.Phase
			}
			stepErr = pauseBatchRun(ctx, run, b, phase, batchPauseCode(stepErr), stepErr)
		}
	}()
	p := batchProviderFactory(run.BaseURL)
	name := "dataark-" + b.UUID
	switch b.Phase {
	case "prepared":
		data, err := batchInput(*b, run)
		if err != nil {
			return failBatch(ctx, run, b, "input preparation failed")
		}
		if err := db.Model(b).Updates(map[string]any{"phase": "upload_unknown", "error": "upload result pending reconciliation"}).Error; err != nil {
			return err
		}
		f, err := p.Upload(ctx, name+".jsonl", data)
		if err != nil {
			if batchPauseCode(err) != 0 {
				return err
			}
			var h batchHTTPError
			if errors.As(err, &h) && h.Status >= 400 && h.Status < 500 && h.Status != 408 && h.Status != 429 {
				return failBatch(ctx, run, b, err.Error())
			}
			return deferBatch(b, "upload_unknown", err)
		}
		return db.Model(b).Updates(map[string]any{"input_file_id": f.ID, "input_jsonl": nil, "phase": "file_processing", "error": "", "next_check_at": time.Now()}).Error
	case "upload_unknown":
		f, err := p.FindFile(ctx, name+".jsonl")
		if err != nil {
			return deferBatch(b, b.Phase, boundedBatchError(b, err))
		}
		if f == nil {
			return deferBatch(b, b.Phase, boundedBatchError(b, fmt.Errorf("upload outcome unknown; no automatic re-upload")))
		}
		return db.Model(b).Updates(map[string]any{"input_file_id": f.ID, "input_jsonl": nil, "phase": "file_processing", "error": "", "next_check_at": time.Now()}).Error
	case "file_processing":
		f, err := p.File(ctx, b.InputFileID)
		if err != nil {
			return deferBatch(b, b.Phase, boundedBatchError(b, err))
		}
		if f.Status == "error" {
			return failBatch(ctx, run, b, "provider rejected input file")
		}
		if f.Status != "processed" {
			return deferBatch(b, b.Phase, nil)
		}
		if err := db.Model(b).Updates(map[string]any{"phase": "submission_unknown", "error": "submission result pending reconciliation"}).Error; err != nil {
			return err
		}
		remote, err := p.Create(ctx, b.InputFileID, name)
		if err != nil {
			if batchPauseCode(err) != 0 {
				return err
			}
			var h batchHTTPError
			if errors.As(err, &h) && h.Status >= 400 && h.Status < 500 && h.Status != 408 && h.Status != 429 {
				return failBatch(ctx, run, b, err.Error())
			}
			return deferBatch(b, "submission_unknown", err)
		}
		return saveRemote(b, remote)
	case "submission_unknown":
		remote, err := p.FindBatch(ctx, b.InputFileID, name)
		if err != nil {
			return deferBatch(b, b.Phase, boundedBatchError(b, err))
		}
		if remote == nil {
			return deferBatch(b, b.Phase, boundedBatchError(b, fmt.Errorf("submission outcome unknown; no automatic resubmission")))
		}
		return saveRemote(b, *remote)
	case "submitted":
		remote, err := p.Retrieve(ctx, b.RemoteID)
		if err != nil {
			created := b.CreatedAt
			if b.SubmittedAt != nil {
				created = *b.SubmittedAt
			}
			if batchHTTPStatus(err) == 404 && !created.IsZero() && time.Since(created) >= 30*24*time.Hour {
				return failBatch(ctx, run, b, "provider task is outside the 30-day query window and returned HTTP 404; existing results retained; retry missing assessments manually")
			}
			return deferBatch(b, b.Phase, boundedBatchError(b, err))
		}
		return saveRemote(b, remote)
	case "importing":
		return importBatch(ctx, run, b, p)
	default:
		return fmt.Errorf("unknown Batch phase %s", b.Phase)
	}
}
func saveRemote(b *Batch, r RemoteBatch) error {
	phase := "submitted"
	switch r.Status {
	case "completed", "failed", "expired", "cancelled":
		phase = "importing"
	}
	interval, _ := time.ParseDuration(config.LLMBATCHPOLLINTERVAL)
	next := time.Now().Add(interval)
	if phase == "importing" {
		next = time.Now()
	}
	now := time.Now().UTC()
	values := map[string]any{"remote_id": r.ID, "provider_status": r.Status, "phase": phase, "output_file_id": r.OutputFileID, "error_file_id": r.ErrorFileID, "completed": r.Counts.Completed, "failed": r.Counts.Failed, "next_check_at": next, "error": batchProblemSummary(batchProviderProblems(r.Errors)), "failure_count": 0}
	if b.SubmittedAt == nil {
		submitted := now
		if r.CreatedAt > 0 {
			submitted = time.Unix(r.CreatedAt, 0).UTC()
		}
		values["submitted_at"] = submitted
	}
	if r.ExpiresAt > 0 {
		values["expires_at"] = time.Unix(r.ExpiresAt, 0).UTC()
	}
	if err := db.Model(b).Updates(values).Error; err != nil {
		return err
	}
	return batchProblemPause(batchProviderProblems(r.Errors))
}
func failBatch(ctx context.Context, run *BatchRun, b *Batch, message string) error {
	var items []BatchItem
	if err := db.WithContext(ctx).Where("batch_id=? AND active_identity IS NOT NULL", b.ID).Find(&items).Error; err != nil {
		return err
	}
	for _, i := range items {
		if err := finishBatchItem(ctx, run, i, "failed", nil, message, nil); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	return db.Model(b).Updates(map[string]any{"phase": "done", "error": message, "finished_at": now, "input_jsonl": nil}).Error
}
func cleanupBatchFiles(ctx context.Context) error {
	paused, err := pausedBatchCleanup(ctx)
	if err != nil || paused != nil {
		return err
	}
	if strings.TrimSpace(config.LLMBATCHAPIKEY) == "" {
		return nil
	}
	var batches []Batch
	if err := db.WithContext(ctx).Omit("input_jsonl").Where("phase='done' AND cleaned_at IS NULL AND finished_at<=? AND run_id NOT IN (SELECT id FROM assessment_batch_runs WHERE status='paused')", time.Now().Add(-7*24*time.Hour)).Order("id").Limit(10).Find(&batches).Error; err != nil {
		return err
	}
	for _, b := range batches {
		var run BatchRun
		if err := db.First(&run, b.RunID).Error; err != nil {
			return err
		}
		if strings.TrimRight(config.LLMBATCHBASEURL, "/") != run.BaseURL {
			continue
		}
		p := batchProviderFactory(run.BaseURL)
		for _, id := range []string{b.InputFileID, b.OutputFileID, b.ErrorFileID} {
			if id == "" {
				continue
			}
			err := p.Delete(ctx, id)
			if err != nil {
				if batchPauseCode(err) != 0 {
					return pauseBatchCleanup(ctx, &run, &b, err)
				}
				var h batchHTTPError
				if !errors.As(err, &h) || h.Status != 404 {
					return err
				}
			}
		}
		if err := db.Model(&Batch{}).Where("id=?", b.ID).Update("cleaned_at", time.Now().UTC()).Error; err != nil {
			return err
		}
	}
	return nil
}

// Lock only while committing an item; network operations never hold this lock.
func lockBatchArticle(tx *gorm.DB, id uint) error { return discovery.LockDedupeMaterialState(tx, id) }

func ReservedBatchTargets(ctx context.Context, targets []jobqueue.AssessmentTarget) (map[jobqueue.AssessmentTarget]bool, error) {
	out := map[jobqueue.AssessmentTarget]bool{}
	if db == nil || !db.Migrator().HasTable(&BatchItem{}) || len(targets) == 0 {
		return out, nil
	}
	ids := make([]uint, 0, len(targets))
	for _, t := range targets {
		ids = append(ids, t.MaterialID)
	}
	var rows []struct {
		MaterialID     uint
		ContentVersion uint
	}
	err := db.WithContext(ctx).Model(&BatchItem{}).Select("material_id,content_version").Where("material_id IN ? AND active_identity IS NOT NULL", ids).Scan(&rows).Error
	for _, r := range rows {
		out[jobqueue.AssessmentTarget{MaterialID: r.MaterialID, ContentVersion: strconv.FormatUint(uint64(r.ContentVersion), 10)}] = true
	}
	return out, err
}
