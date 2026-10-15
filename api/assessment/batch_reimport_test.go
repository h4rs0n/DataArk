package assessment

import (
	"DataArk/config"
	"DataArk/material"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func compatibleBatchFixtureContent() string {
	var fields map[string]any
	_ = json.Unmarshal([]byte(validBatchAssessment), &fields)
	fields["reasons"] = "文章解释队列机制并提供具体证据。局限在于未比较其他方案。"
	fields["keywords"] = "队列、调度、评估"
	fields["metadata"] = "ignored"
	content, _ := json.Marshal(fields)
	return string(content)
}

func TestBatchImportCompatibleResultsAreActivatedOnce(t *testing.T) {
	p, _, candidate := setupBatchFixture(t)
	ctx := context.Background()
	_ = StartBatchRun(ctx)
	reachSubmittedBatch(t, p)
	var item BatchItem
	db.First(&item)
	p.output = batchResult(item.CustomID, compatibleBatchFixtureContent())
	batchStep(t)
	batchStep(t)
	db.First(&item, item.ID)
	var state material.ArticleState
	db.First(&state, "material_id=?", candidate.MaterialID)
	if item.Status != "succeeded" || item.AssessmentID == nil || state.CurrentAssessmentID == nil || state.AssessmentError != "" {
		t.Fatal(item, state)
	}
	var assessment ArticleAssessment
	db.First(&assessment, *item.AssessmentID)
	var call LLMCall
	db.Where("batch_item_id=?", item.ID).First(&call)
	if call.ResponseMode != ResponseSchemaMode {
		t.Fatal("new Batch call lost its JSON Schema mode", call.ResponseMode)
	}
	var reasons []string
	if json.Unmarshal([]byte(assessment.Reasons), &reasons) != nil || len(reasons) != 1 || !strings.Contains(reasons[0], "局限") {
		t.Fatal(assessment)
	}
	var batch Batch
	db.First(&batch)
	var run BatchRun
	db.First(&run)
	if err := importBatchStream(ctx, &run, &batch, strings.NewReader(p.output)); err != nil {
		t.Fatal(err)
	}
	var calls int64
	db.Model(&LLMCall{}).Where("batch_item_id=?", item.ID).Count(&calls)
	if calls != 1 || p.creates != 1 {
		t.Fatal(calls, p.creates)
	}
}

func failedReimportFixture(t *testing.T, p *fixtureBatchProvider) (BatchRun, Batch, BatchItem, LLMCall) {
	t.Helper()
	now := time.Now().UTC()
	candidate := createAssessmentCandidate(t, "Reimport", fmt.Sprintf("https://batch.invalid/reimport-%d", now.UnixNano()), "Reimport response", strings.Repeat("existing inference evidence ", 100), now)
	run := BatchRun{Status: "done", BaseURL: config.LLMBATCHBASEURL, Model: config.LLMBATCHMODEL, PolicyVersion: ArticleQualityPolicyVersion, CreatedAt: now, FinishedAt: &now}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	batch := Batch{RunID: run.ID, UUID: fmt.Sprintf("reimport-%d", run.ID), Phase: "done", OutputFileID: "existing-output", CreatedAt: now, NextCheckAt: now, FinishedAt: &now}
	if err := db.Create(&batch).Error; err != nil {
		t.Fatal(err)
	}
	identity := fmt.Sprintf("%d:1", candidate.MaterialID)
	item := BatchItem{RunID: run.ID, BatchID: &batch.ID, JobID: "fixture", MaterialID: candidate.MaterialID, CandidateID: candidate.ID, ContentVersion: 1, CustomID: fmt.Sprintf("reimport-item-%d", run.ID), Status: "processing", ActiveIdentity: &identity, CreatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	usage := batchUsage{Prompt: 100, Completion: 10, Total: 110}
	if err := finishBatchItem(context.Background(), &run, item, "failed", nil, "model output is invalid JSON or was truncated", &usage); err != nil {
		t.Fatal(err)
	}
	var call LLMCall
	if err := db.Where("batch_item_id=?", item.ID).First(&call).Error; err != nil {
		t.Fatal(err)
	}
	db.First(&item, item.ID)
	p.output = batchResult(item.CustomID, compatibleBatchFixtureContent())
	return run, batch, item, call
}

func verifyBatchCompatibilityReimport(t *testing.T, p *fixtureBatchProvider) {
	t.Helper()
	run, _, item, call := failedReimportFixture(t, p)
	ctx := context.Background()
	beforeCreates := p.creates
	preview, err := ReimportBatchRun(ctx, run.ID, true)
	if err != nil || preview.Recoverable != 1 || preview.Restored != 0 {
		t.Fatal(preview, err)
	}
	var unchanged BatchItem
	db.First(&unchanged, item.ID)
	if unchanged.Status != "failed" {
		t.Fatal("preview changed state", unchanged)
	}
	applied, err := ReimportBatchRun(ctx, run.ID, false)
	if err != nil || applied.Restored != 1 {
		t.Fatal(applied, err)
	}
	var restored BatchItem
	db.First(&restored, item.ID)
	var updated LLMCall
	db.Where("batch_item_id=?", item.ID).First(&updated)
	if restored.Status != "succeeded" || restored.Error != "" || restored.AssessmentID == nil || restored.ActiveIdentity != nil {
		t.Fatal(restored)
	}
	if updated.ID != call.ID || updated.TotalTokens != call.TotalTokens || !updated.CreatedAt.Equal(call.CreatedAt) || updated.Status != "success" || updated.ErrorType != "" || updated.ResponseMode != ResponseObjectMode {
		t.Fatal("call accounting changed", call, updated)
	}
	var count int64
	db.Model(&LLMCall{}).Where("batch_item_id=?", item.ID).Count(&count)
	if count != 1 || p.creates != beforeCreates {
		t.Fatal("reimport repeated inference", count, p.creates)
	}
	requests := p.requests
	repeated, err := ReimportBatchRun(ctx, run.ID, false)
	if err != nil || repeated.Examined != 0 || repeated.Restored != 0 || p.requests != requests {
		t.Fatal("reimport is not idempotent", repeated, err)
	}
}

func TestBatchReimportRestoresExistingOutputsWithoutNewInference(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	verifyBatchCompatibilityReimport(t, p)
}

func TestBatchReimportPreservesChangedAndReservedArticles(t *testing.T) {
	for _, guard := range []string{"version", "reservation", "returned", "new-assessment", "invalid"} {
		t.Run(guard, func(t *testing.T) {
			p, _, _ := setupBatchFixture(t)
			run, _, item, _ := failedReimportFixture(t, p)
			switch guard {
			case "version":
				db.Model(&material.ArticleState{}).Where("material_id=?", item.MaterialID).Update("content_version", 2)
			case "reservation":
				identity := fmt.Sprintf("%d:1", item.MaterialID)
				other := BatchItem{RunID: run.ID, JobID: "other", MaterialID: item.MaterialID, ContentVersion: 1, CustomID: "other", Status: "pending", ActiveIdentity: &identity, CreatedAt: time.Now()}
				if err := db.Create(&other).Error; err != nil {
					t.Fatal(err)
				}
			case "returned":
				db.Model(&item).Update("returned_at", time.Now())
			case "new-assessment":
				row := ArticleAssessment{MaterialID: item.MaterialID, ContentVersion: 1, Assessor: "bailian_batch", AssessorVersion: "new-model", PolicyVersion: ArticleQualityPolicyVersion}
				if err := db.Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				db.Model(&material.ArticleState{}).Where("material_id=?", item.MaterialID).Update("current_assessment_id", row.ID)
			case "invalid":
				p.output = batchResult(item.CustomID, `{"qualityScore":999}`)
			}
			stats, err := ReimportBatchRun(context.Background(), run.ID, false)
			if err != nil || stats.Restored != 0 {
				t.Fatal(stats, err)
			}
			var persisted BatchItem
			db.First(&persisted, item.ID)
			if persisted.Status != "failed" || persisted.AssessmentID != nil {
				t.Fatal("unsafe result activated", persisted)
			}
			if guard == "invalid" && !strings.Contains(persisted.Error, "Missing required fields") {
				t.Fatal("precise validation error was lost", persisted.Error)
			}
		})
	}
}

func TestBatchReimportAuthFailureStopsImmediately(t *testing.T) {
	for _, code := range []int{401, 429} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			p, _, _ := setupBatchFixture(t)
			run, _, _, _ := failedReimportFixture(t, p)
			one := 1
			db.Model(&run).Updates(map[string]any{"active_key": &one, "status": "preparing"})
			p.operationErrors = map[string]error{"download": batchHTTPError{Status: code}}
			before := p.requests
			if _, err := ReimportBatchRun(context.Background(), run.ID, false); err == nil {
				t.Fatal("auth failure ignored")
			}
			var paused BatchRun
			db.First(&paused, run.ID)
			if paused.Status != "paused" || paused.ErrorCode != code || p.requests != before+1 {
				t.Fatal(paused, p.requests)
			}
		})
	}
}
