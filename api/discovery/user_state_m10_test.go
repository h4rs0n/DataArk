package discovery

import (
	"testing"
	"time"
)

func TestUserCandidateStateDoesNotMutateSharedCandidateOrOtherUsers(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	candidate := DiscoveryCandidate{
		SourceID: 1, SourceName: "Shared Feed", URL: "https://shared.example/post",
		Title: "Shared article", Status: DiscoveryCandidateStatusNew,
		ProcessingState: DiscoveryProcessingReady, EligibilityState: DiscoveryEligibilityEligible,
		DedupeState: DiscoveryDedupeReady, LastSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := MarkUserCandidateRead(101, candidate.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkUserCandidateIgnored(101, candidate.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkUserCandidateArchived(101, candidate.ID, "archive-task-1"); err != nil {
		t.Fatal(err)
	}

	var shared DiscoveryCandidate
	if err := db.First(&shared, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if shared.Status != DiscoveryCandidateStatusNew {
		t.Fatalf("shared status = %q, want unchanged new", shared.Status)
	}
	if shared.ArchivedTaskID != "archive-task-1" {
		t.Fatalf("shared archive task = %q, want completed shared artifact reference", shared.ArchivedTaskID)
	}

	userA, err := GetUserCandidateState(101, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if userA.ReadAt == nil || userA.ArchivedAt == nil || userA.CurrentFeedback != UserCandidateFeedbackNotInterested {
		t.Fatalf("user A state = %#v", userA)
	}
	if _, err := GetUserCandidateState(202, candidate.ID); err == nil {
		t.Fatal("user B unexpectedly inherited user A state")
	}

	userBCandidates, err := ListDiscoveryCandidatesForUser(202, DiscoveryCandidateStatusNew, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(userBCandidates) != 1 || userBCandidates[0].ID != candidate.ID || userBCandidates[0].UserState != nil {
		t.Fatalf("user B candidates = %#v", userBCandidates)
	}
	userAIgnored, err := ListDiscoveryCandidatesForUser(101, DiscoveryCandidateStatusIgnored, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(userAIgnored) != 1 || userAIgnored[0].ID != candidate.ID {
		t.Fatalf("user A ignored candidates = %#v", userAIgnored)
	}
}

func TestRecordUserCandidateExposureIsIsolatedAndIdempotentlyCounted(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)
	candidate := DiscoveryCandidate{SourceID: 1, URL: "https://shared.example/exposure", Status: DiscoveryCandidateStatusNew, LastSeenAt: now}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	if err := RecordUserCandidateExposure(db, 101, candidate.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := RecordUserCandidateExposure(db, 101, candidate.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := RecordUserCandidateExposure(db, 202, candidate.ID, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	userA, err := GetUserCandidateState(101, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	userB, err := GetUserCandidateState(202, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if userA.ExposureCount != 2 || userB.ExposureCount != 1 {
		t.Fatalf("exposure counts: user A=%d user B=%d", userA.ExposureCount, userB.ExposureCount)
	}
	if userA.FirstExposedAt == nil || !userA.FirstExposedAt.Equal(now) || userA.LastExposedAt == nil || !userA.LastExposedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("user A exposure timestamps = %#v", userA)
	}
}
