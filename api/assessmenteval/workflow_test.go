package assessmenteval

import (
	"DataArk/articlevalue"
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
	for index, item := range manifest.Items[1:29] {
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
	summary, err = SkipWorkflowItem(database, run.ID, 1, 1, manifest.Items[29].SampleID, now.Add(30*time.Minute))
	if err != nil || summary.PassOne.Labeled != 29 || summary.PassOne.Skipped != 1 || summary.CanAdvance {
		t.Fatalf("skip before minimum: summary=%#v err=%v", summary, err)
	}
	if _, err := AdvanceWorkflow(database, run.ID, now.Add(30*time.Minute)); err == nil {
		t.Fatal("pass one advanced with fewer than the minimum labels")
	}
	skippedItem, err := GetWorkflowItem(database, run.ID, 1, 29)
	if err != nil || !skippedItem.Skipped {
		t.Fatalf("persisted skip: item=%#v err=%v", skippedItem, err)
	}
	thirtiethScores := fixtureScores(29)
	summary, err = SaveWorkflowLabel(database, run.ID, 1, 1, manifest.Items[29].SampleID, WorkflowLabelInput{
		Scores: &thirtiethScores, Reason: "replaced skip", Genre: "analysis",
	}, now.Add(31*time.Minute))
	if err != nil || summary.PassOne.Labeled != MinimumPassOneLabels || summary.PassOne.Skipped != 0 || !summary.CanAdvance {
		t.Fatalf("minimum labels replace skip: summary=%#v err=%v", summary, err)
	}
	restoredItem, err := GetWorkflowItem(database, run.ID, 1, 29)
	if err != nil || restoredItem.Skipped {
		t.Fatalf("saved label did not clear skip: item=%#v err=%v", restoredItem, err)
	}
	summary, err = SkipWorkflowItem(database, run.ID, 1, 1, manifest.Items[29].SampleID, now.Add(32*time.Minute))
	if err != nil || summary.PassOne.Labeled != MinimumPassOneLabels-1 || summary.PassOne.Skipped != 1 || summary.CanAdvance {
		t.Fatalf("skip did not replace existing label: summary=%#v err=%v", summary, err)
	}
	summary, err = SaveWorkflowLabel(database, run.ID, 1, 1, manifest.Items[29].SampleID, WorkflowLabelInput{
		Scores: &thirtiethScores, Reason: "restored label", Genre: "analysis",
	}, now.Add(33*time.Minute))
	if err != nil || summary.PassOne.Labeled != MinimumPassOneLabels || summary.PassOne.Skipped != 0 || !summary.CanAdvance {
		t.Fatalf("restored minimum label: summary=%#v err=%v", summary, err)
	}
	summary, err = SkipWorkflowItem(database, run.ID, 1, 1, manifest.Items[30].SampleID, now.Add(34*time.Minute))
	if err != nil || summary.PassOne.Labeled != MinimumPassOneLabels || summary.PassOne.Skipped != 1 || !summary.CanAdvance {
		t.Fatalf("optional skip after minimum: summary=%#v err=%v", summary, err)
	}
	summary, err = AdvanceWorkflow(database, run.ID, now.Add(2*time.Hour))
	if err != nil || summary.Status != WorkflowStatusWaitingPassTwo || summary.NextPassAvailableAt == nil || summary.PassOne.Skipped != GoldSampleCount-MinimumPassOneLabels {
		t.Fatalf("complete pass one: summary=%#v err=%v", summary, err)
	}
	if _, err := GetWorkflowItem(database, run.ID, 1, 0); err == nil {
		t.Fatal("frozen pass-one labels remained readable during the blind interval")
	}
	if _, err := AdvanceWorkflow(database, run.ID, summary.NextPassAvailableAt.Add(-time.Minute)); err == nil {
		t.Fatal("pass two started before the 72-hour blind interval")
	}
	summary, err = AdvanceWorkflow(database, run.ID, *summary.NextPassAvailableAt)
	if err != nil || summary.Status != WorkflowStatusPassTwo || summary.PassTwo.Total != PassTwoSampleCount {
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
	for _, item := range repeated {
		if _, exists := firstLabels[item.SampleID]; !exists {
			t.Fatalf("pass two selected skipped or unlabelled sample %q", item.SampleID)
		}
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
		Seed: manifest.Seed, ManifestDigest: manifest.Digest, PolicyVersion: articlevalue.PolicyVersion,
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
