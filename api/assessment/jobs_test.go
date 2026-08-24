package assessment

import (
	"DataArk/discovery"
	"context"
	"testing"
	"time"
)

type recoveryQueue struct {
	assessments []uint
}

func (queue *recoveryQueue) EnqueueAssessArticle(_ context.Context, candidateID uint, _ string) error {
	queue.assessments = append(queue.assessments, candidateID)
	return nil
}

func TestRecoverDueJobsEnqueuesPendingAssessments(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	candidate := discovery.DiscoveryCandidate{
		SourceID: 1, SourceName: "Feed", URL: "https://assess.example/ready",
		Status: discovery.DiscoveryCandidateStatusNew, ProcessingState: discovery.DiscoveryProcessingReady,
		DedupeState: discovery.DiscoveryDedupeReady, AssessmentState: discovery.DiscoveryAssessmentPending,
		ContentVersion: 1, EligibilityState: discovery.DiscoveryEligibilityUnknown, LastSeenAt: now,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	queue := &recoveryQueue{}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(queue.assessments) != 1 || queue.assessments[0] != candidate.ID {
		t.Fatalf("assessment recoveries = %#v", queue.assessments)
	}
}

func TestEnqueuePendingEnqueuesOnlyPendingCandidates(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 18, 11, 0, 0, 0, time.UTC)
	pending := discovery.DiscoveryCandidate{
		SourceID: 1, SourceName: "Feed", URL: "https://assess.example/pending",
		Status: discovery.DiscoveryCandidateStatusNew, ProcessingState: discovery.DiscoveryProcessingReady,
		DedupeState: discovery.DiscoveryDedupeReady, AssessmentState: discovery.DiscoveryAssessmentPending,
		ContentVersion: 3, EligibilityState: discovery.DiscoveryEligibilityUnknown, LastSeenAt: now,
	}
	ready := discovery.DiscoveryCandidate{
		SourceID: 1, SourceName: "Feed", URL: "https://assess.example/ready",
		Status: discovery.DiscoveryCandidateStatusNew, ProcessingState: discovery.DiscoveryProcessingReady,
		DedupeState: discovery.DiscoveryDedupeReady, AssessmentState: discovery.DiscoveryAssessmentReady,
		ContentVersion: 2, EligibilityState: discovery.DiscoveryEligibilityEligible, LastSeenAt: now,
	}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ready).Error; err != nil {
		t.Fatal(err)
	}
	queue := &recoveryQueue{}
	if err := EnqueuePending(context.Background(), queue, pending.ID); err != nil {
		t.Fatal(err)
	}
	if err := EnqueuePending(context.Background(), queue, ready.ID); err != nil {
		t.Fatal(err)
	}
	if len(queue.assessments) != 1 || queue.assessments[0] != pending.ID {
		t.Fatalf("enqueued = %#v, want only pending %d", queue.assessments, pending.ID)
	}
}
