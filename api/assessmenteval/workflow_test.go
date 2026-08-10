package assessmenteval

import (
	"DataArk/discovery"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDatabaseWorkflowPersistsBlindPassesDelayAndAdjudication(t *testing.T) {
	database := workflowTestDatabase(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	manifest, err := BuildManifest(syntheticCandidateRecords(), "workflow-seed", now)
	if err != nil {
		t.Fatal(err)
	}
	run := seedWorkflow(t, database, manifest, now)

	firstItem := manifest.Items[0]
	firstScores := fixtureScores(0)
	summary, err := SaveWorkflowLabel(database, run.ID, 1, 1, firstItem.SampleID, WorkflowLabelInput{
		Scores: &firstScores, Reason: "first pass", Genre: "analysis", DurationSeconds: 12,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if summary.PassOne.Labeled != 1 {
		t.Fatalf("first-pass progress = %#v", summary.PassOne)
	}
	for index, item := range manifest.Items[1:] {
		scores := fixtureScores(index + 1)
		label := ArticleAssessmentWorkflowLabel{
			RunID: run.ID, SampleID: item.SampleID, Pass: 1,
			Quality: scores.Quality, Depth: scores.Depth, Evergreen: scores.Evergreen,
			Reason: "fixture", Genre: "analysis", LabeledBy: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := database.Create(&label).Error; err != nil {
			t.Fatal(err)
		}
	}
	summary, err = AdvanceWorkflow(database, run.ID, now.Add(2*time.Hour))
	if err != nil || summary.Status != WorkflowStatusWaitingPassTwo || summary.NextPassAvailableAt == nil {
		t.Fatalf("complete pass one: summary=%#v err=%v", summary, err)
	}
	if _, err := GetWorkflowItem(database, run.ID, 1, 0); err == nil {
		t.Fatal("frozen pass-one labels remained readable during the blind interval")
	}
	if _, err := AdvanceWorkflow(database, run.ID, summary.NextPassAvailableAt.Add(-time.Minute)); err == nil {
		t.Fatal("pass two started before the 72-hour blind interval")
	}
	summary, err = AdvanceWorkflow(database, run.ID, *summary.NextPassAvailableAt)
	if err != nil || summary.Status != WorkflowStatusPassTwo || summary.PassTwo.Total != 30 {
		t.Fatalf("start pass two: summary=%#v err=%v", summary, err)
	}
	blindItem, err := GetWorkflowItem(database, run.ID, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(blindItem)
	if strings.Contains(string(payload), "baseline") || strings.Contains(string(payload), "host-") || strings.Contains(string(payload), "stratum") {
		t.Fatalf("blind response exposed hidden evidence: %s", payload)
	}

	firstLabels, err := loadWorkflowLabelMap(database, run.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var repeated []ArticleAssessmentWorkflowItem
	if err := database.Where("run_id = ? AND pass_two_position IS NOT NULL", run.ID).Order("pass_two_position").Find(&repeated).Error; err != nil {
		t.Fatal(err)
	}
	for index, item := range repeated {
		label := firstLabels[item.SampleID]
		if index == 0 {
			label.Scores.Quality = (label.Scores.Quality + 40) % 101
			if !scoresConflict(firstLabels[item.SampleID].Scores, label.Scores) {
				label.Scores.Quality = 100
			}
		}
		row := ArticleAssessmentWorkflowLabel{
			RunID: run.ID, SampleID: item.SampleID, Pass: 2,
			Quality: label.Scores.Quality, Depth: label.Scores.Depth, Evergreen: label.Scores.Evergreen,
			Reason: "repeat", Genre: "analysis", LabeledBy: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := database.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	summary, err = AdvanceWorkflow(database, run.ID, now.Add(80*time.Hour))
	if err != nil || summary.Status != WorkflowStatusAdjudication || summary.Adjudication.Total != 1 {
		t.Fatalf("complete pass two: summary=%#v err=%v", summary, err)
	}
	conflict, err := GetWorkflowItem(database, run.ID, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	resolved := AxisScores{Quality: 60, Depth: 60, Evergreen: 60}
	summary, err = SaveWorkflowLabel(database, run.ID, 1, 3, conflict.SampleID, WorkflowLabelInput{
		Scores: &resolved, Reason: "adjudicated", Genre: "analysis",
	}, now.Add(81*time.Hour))
	if err != nil || !summary.CanAdvance {
		t.Fatalf("save adjudication: summary=%#v err=%v", summary, err)
	}
	summary, err = AdvanceWorkflow(database, run.ID, now.Add(82*time.Hour))
	if err != nil || summary.Status != WorkflowStatusHumanComplete || !summary.CanEvaluate {
		t.Fatalf("complete human gold: summary=%#v err=%v", summary, err)
	}
}

func TestWorkflowLabelRequiresCompleteSchema(t *testing.T) {
	if err := validateWorkflowLabel(WorkflowLabelInput{Reason: "missing", Genre: "analysis"}); err == nil {
		t.Fatal("missing scores were accepted")
	}
	invalid := AxisScores{Quality: 101, Depth: 1, Evergreen: 1}
	if err := validateWorkflowLabel(WorkflowLabelInput{Scores: &invalid, Reason: "invalid", Genre: "analysis"}); err == nil {
		t.Fatal("out-of-range score was accepted")
	}
	if err := validateWorkflowLabel(WorkflowLabelInput{Unjudgeable: true}); err != nil {
		t.Fatalf("unjudgeable item should not require scores: %v", err)
	}
}

func workflowTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models := []interface{}{&discovery.DiscoveryCandidate{}, &discovery.DiscoveryArticleContentVersion{}}
	models = append(models, WorkflowModels()...)
	if err := database.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	return database
}

func seedWorkflow(t *testing.T, database *gorm.DB, manifest Manifest, now time.Time) ArticleAssessmentWorkflowRun {
	t.Helper()
	run := ArticleAssessmentWorkflowRun{
		Seed: manifest.Seed, ManifestDigest: manifest.Digest, PolicyVersion: "article-value-v3",
		Status: WorkflowStatusPassOne, CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	for position, item := range manifest.Items {
		candidate := discovery.DiscoveryCandidate{ID: item.CandidateID, SourceID: 1, URL: fmt.Sprintf("https://fixture.invalid/%d", item.CandidateID), ContentVersion: item.ContentVersion, CreatedAt: now, UpdatedAt: now, LastSeenAt: now}
		_ = database.Create(&candidate).Error
		content := discovery.DiscoveryArticleContentVersion{
			ID: item.ContentVersionID, CandidateID: item.CandidateID, ContentVersion: item.ContentVersion,
			ContentHash: item.ContentHash, Title: item.Title, BodyText: item.BodyText, Language: item.Language,
			FetchedAt: now, CreatedAt: now,
		}
		if err := database.Create(&content).Error; err != nil {
			t.Fatalf("create immutable content %s: %v", item.SampleID, err)
		}
		workflowItem := workflowItemFromManifest(run.ID, position, item, now)
		if err := database.Create(&workflowItem).Error; err != nil {
			t.Fatal(err)
		}
	}
	return run
}
