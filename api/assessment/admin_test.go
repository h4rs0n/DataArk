package assessment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"DataArk/discovery"
)

type modeFixtureAssessor struct {
	active bool
	result ArticleAssessmentResult
}

func (modeFixtureAssessor) Name() string          { return "admin_fixture" }
func (modeFixtureAssessor) Version() string       { return "fixture-model" }
func (modeFixtureAssessor) PolicyVersion() string { return ArticleQualityPolicyVersion }
func (assessor modeFixtureAssessor) ShouldActivateAssessment() bool {
	return assessor.active
}
func (assessor modeFixtureAssessor) Assess(context.Context, ArticleAssessmentInput) (ArticleAssessmentResult, error) {
	return assessor.result, nil
}

type recordedJobs struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

type recordingJobEnqueuer struct {
	store *recordedJobs
}

func (queue recordingJobEnqueuer) enqueue(key string) error {
	queue.store.mu.Lock()
	defer queue.store.mu.Unlock()
	queue.store.keys[key] = struct{}{}
	return nil
}

func (queue recordingJobEnqueuer) EnqueueAssessArticle(_ context.Context, candidateID uint, contentVersion string) error {
	return queue.enqueue(fmt.Sprintf("assess:%d:%s", candidateID, contentVersion))
}

type failingAssessmentQueue struct {
	recordingJobEnqueuer
	failCandidate uint
}

func (queue failingAssessmentQueue) EnqueueAssessArticle(ctx context.Context, candidateID uint, contentVersion string) error {
	if candidateID == queue.failCandidate {
		return errors.New("fixture queue failure")
	}
	return queue.recordingJobEnqueuer.EnqueueAssessArticle(ctx, candidateID, contentVersion)
}

func TestArticleAssessmentBackfillReactivatesObservedRowsWithoutCallingModel(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	candidate := createAssessmentCandidate(t, "Fixture", "https://example.com/observed", "Observed article", "A durable analysis with enough evidence to be assessed independently.", now)
	result := ArticleAssessmentResult{Quality: .82, Depth: .74, Evergreen: .68, Confidence: .9, Reasons: []string{"clear evidence", "bounded limitation"}}
	if err := AssessCandidate(context.Background(), candidate.ID, modeFixtureAssessor{active: false, result: result}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalAssessmentID := *candidate.CurrentAssessmentID

	dry, err := PrepareArticleAssessmentBackfill(context.Background(), modeFixtureAssessor{active: true, result: result}, nil, ArticleAssessmentBatchOptions{Limit: 1, DryRun: true})
	if err != nil || dry.Selected != 1 || dry.Reactivated != 1 || dry.Enqueued != 0 {
		t.Fatalf("dry result=%#v err=%v", dry, err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil || *candidate.CurrentAssessmentID != originalAssessmentID {
		t.Fatalf("dry run changed candidate: %#v err=%v", candidate, err)
	}

	store := &recordedJobs{keys: make(map[string]struct{})}
	actual, err := PrepareArticleAssessmentBackfill(context.Background(), modeFixtureAssessor{active: true, result: result}, recordingJobEnqueuer{store: store}, ArticleAssessmentBatchOptions{Limit: 1})
	if err != nil || actual.Reactivated != 1 || actual.Enqueued != 0 || len(store.keys) != 0 {
		t.Fatalf("actual result=%#v jobs=%#v err=%v", actual, store.keys, err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	var active ArticleAssessment
	if err := db.First(&active, *candidate.CurrentAssessmentID).Error; err != nil {
		t.Fatal(err)
	}
	if active.Assessor != "admin_fixture" || candidate.QualityScore != .82 {
		t.Fatalf("active assessment=%#v candidate=%#v", active, candidate)
	}
	repeated, err := PrepareArticleAssessmentBackfill(context.Background(), modeFixtureAssessor{active: true, result: result}, recordingJobEnqueuer{store: store}, ArticleAssessmentBatchOptions{Limit: 1})
	if err != nil || repeated.Selected != 0 || repeated.Enqueued != 0 || repeated.Reactivated != 0 {
		t.Fatalf("idempotent replay result=%#v err=%v", repeated, err)
	}
}

func TestArticleAssessmentBackfillPreservesPointerAcrossPartialQueueFailure(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	first := createAssessmentCandidate(t, "Fixture", "https://example.com/first", "First", "First article body with enough deterministic evidence.", now)
	second := createAssessmentCandidate(t, "Fixture", "https://example.com/second", "Second", "Second article body with enough deterministic evidence.", now)
	for _, candidate := range []discovery.DiscoveryCandidate{first, second} {
		if err := AssessCandidate(context.Background(), candidate.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalFirstAssessmentID := *first.CurrentAssessmentID
	store := &recordedJobs{keys: make(map[string]struct{})}
	assessor := modeFixtureAssessor{active: true, result: ArticleAssessmentResult{Quality: .8, Depth: .7, Evergreen: .6, Confidence: .9, Reasons: []string{"one", "two"}}}
	result, err := PrepareArticleAssessmentBackfill(context.Background(), assessor, failingAssessmentQueue{recordingJobEnqueuer: recordingJobEnqueuer{store: store}, failCandidate: first.ID}, ArticleAssessmentBatchOptions{Limit: 2})
	if err == nil || result.Selected != 2 || result.Enqueued != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if err := db.First(&first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.CurrentAssessmentID == nil || *first.CurrentAssessmentID != originalFirstAssessmentID || first.AssessmentState != discovery.DiscoveryAssessmentPending {
		t.Fatalf("failed enqueue lost active pointer: %#v", first)
	}
	if _, ok := store.keys[fmt.Sprintf("assess:%d:1", second.ID)]; !ok {
		t.Fatalf("successful job missing: %#v", store.keys)
	}
}

func TestArticleAssessmentRollbackRestoresDeterministicRow(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	candidate := createAssessmentCandidate(t, "Fixture", "https://example.com/rollback", "Rollback", "A detailed article body that supports rollback testing.", now)
	assessor := modeFixtureAssessor{active: true, result: ArticleAssessmentResult{Quality: .9, Depth: .8, Evergreen: .7, Confidence: .9, Reasons: []string{"one", "two"}}}
	if err := AssessCandidate(context.Background(), candidate.ID, assessor); err != nil {
		t.Fatal(err)
	}
	result, err := RollbackArticleAssessment(context.Background(), assessor, ArticleAssessmentBatchOptions{Limit: 1})
	if err != nil || result.Selected != 1 || result.Reactivated != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	var active ArticleAssessment
	if err := db.First(&active, *candidate.CurrentAssessmentID).Error; err != nil {
		t.Fatal(err)
	}
	if active.Assessor != RuleArticleAssessorName || candidate.AssessmentState != discovery.DiscoveryAssessmentDegraded {
		t.Fatalf("rollback active=%#v candidate=%#v", active, candidate)
	}
}

func TestArticleAssessmentBackfillObserveEnqueuesWithoutActivating(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC)
	candidate := createAssessmentCandidate(t, "Fixture", "https://example.com/observe-backfill", "Observe backfill", "A durable analysis with enough evidence to be assessed independently.", now)
	if err := AssessCandidate(context.Background(), candidate.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalAssessmentID := *candidate.CurrentAssessmentID
	store := &recordedJobs{keys: make(map[string]struct{})}
	assessor := modeFixtureAssessor{active: false, result: ArticleAssessmentResult{Quality: .8, Depth: .7, Evergreen: .6, Confidence: .9, Reasons: []string{"one", "two"}, Summary: "Queued summary", Keywords: []string{"go", "llm", "testing"}}}
	result, err := PrepareArticleAssessmentBackfill(context.Background(), assessor, recordingJobEnqueuer{store: store}, ArticleAssessmentBatchOptions{Limit: 1})
	if err != nil || result.Selected != 1 || result.Enqueued != 1 || result.Reactivated != 0 {
		t.Fatalf("observe backfill result=%#v err=%v", result, err)
	}
	if _, ok := store.keys[fmt.Sprintf("assess:%d:1", candidate.ID)]; !ok {
		t.Fatalf("observe enqueue missing: %#v", store.keys)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.CurrentAssessmentID == nil || *candidate.CurrentAssessmentID != originalAssessmentID {
		t.Fatalf("observe backfill activated scores: %#v", candidate)
	}
}

func TestArticleAssessmentBackfillObserveSkipsPersistedModelRows(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 17, 13, 30, 0, 0, time.UTC)
	candidate := createAssessmentCandidate(t, "Fixture", "https://example.com/observe-skip", "Observe skip", "A durable analysis with enough evidence to be assessed independently.", now)
	result := ArticleAssessmentResult{Quality: .82, Depth: .74, Evergreen: .68, Confidence: .9, Reasons: []string{"clear evidence", "bounded limitation"}, Summary: "Persisted summary", Keywords: []string{"testing", "evidence", "methods"}}
	if err := AssessCandidate(context.Background(), candidate.ID, modeFixtureAssessor{active: false, result: result}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalAssessmentID := *candidate.CurrentAssessmentID
	store := &recordedJobs{keys: make(map[string]struct{})}
	backfill, err := PrepareArticleAssessmentBackfill(context.Background(), modeFixtureAssessor{active: false, result: result}, recordingJobEnqueuer{store: store}, ArticleAssessmentBatchOptions{Limit: 1})
	if err != nil || backfill.Selected != 0 || backfill.Enqueued != 0 || backfill.Reactivated != 0 || len(store.keys) != 0 {
		t.Fatalf("observe skip result=%#v jobs=%#v err=%v", backfill, store.keys, err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if *candidate.CurrentAssessmentID != originalAssessmentID {
		t.Fatalf("observe skip changed pointer: %#v", candidate)
	}
}
