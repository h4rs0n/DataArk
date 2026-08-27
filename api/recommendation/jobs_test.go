package recommendation

import (
	"DataArk/jobqueue"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRecoverDueJobsEnqueuesMissingDraftAndFailedLocalDay(t *testing.T) {
	setupSQLiteDB(t)
	for _, userID := range []uint{41, 42, 43, 44} {
		settings := DefaultRecommendationSettings(userID)
		settings.Enabled = true
		settings.Timezone = "Asia/Shanghai"
		settings.GenerationTime = "07:00"
		if _, err := SaveRecommendationSettings(&settings); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CreateRecommendationDay(42, "2026-07-13", 10); err != nil {
		t.Fatal(err)
	}
	failed, err := CreateRecommendationDay(43, "2026-07-13", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&RecommendationDay{}).Where("id = ?", failed.ID).Update("status", RecommendationDayStatusFailed).Error; err != nil {
		t.Fatal(err)
	}
	published, err := CreateRecommendationDay(44, "2026-07-13", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&RecommendationDay{}).Where("id = ?", published.ID).Update("status", RecommendationDayStatusPublished).Error; err != nil {
		t.Fatal(err)
	}

	var users []uint
	queue := jobqueue.NewMemoryQueue(jobqueue.NewMemoryStore(), jobqueue.Handlers{
		GenerateDaily: func(_ context.Context, userID uint, localDate string) error {
			if localDate != "2026-07-13" {
				t.Fatalf("local date = %q", localDate)
			}
			users = append(users, userID)
			day, err := CreateRecommendationDay(userID, localDate, 10)
			if err != nil {
				return err
			}
			return db.Model(&RecommendationDay{}).Where("id = ?", day.ID).Update("status", RecommendationDayStatusPublished).Error
		},
	})
	now := time.Date(2026, 7, 13, 0, 30, 0, 0, time.UTC)
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 || users[0] != 41 || users[1] != 42 || users[2] != 43 {
		t.Fatalf("recovered users = %#v", users)
	}
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 {
		t.Fatalf("published recovery repeated: %#v", users)
	}
}

func TestRecoverDueJobsEnqueuesStaleDigestSummaries(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC))
	fresh := seedPublishedDayWithCount(t, 45, "2026-07-13", 2)
	if err := db.Model(&RecommendationDay{}).Where("id = ?", fresh.ID).Updates(map[string]interface{}{
		"summary_text": "已有摘要", "summary_actual_count": 2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	seedPublishedDayWithCount(t, 46, "2026-07-12", 3)
	seedPublishedDayWithCount(t, 47, "2026-07-11", 1)
	seedPublishedDayWithCount(t, 48, "2026-07-10", 0)
	if err := db.Model(&RecommendationSettings{}).Where("user_id IN ?", []uint{46, 47, 48}).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	var keys []string
	queue := jobqueue.NewMemoryQueue(jobqueue.NewMemoryStore(), jobqueue.Handlers{
		GenerateDaily: func(context.Context, uint, string) error {
			t.Fatal("published days must not re-enqueue generate daily")
			return nil
		},
		GenerateDigestSummary: func(_ context.Context, userID uint, localDate string) error {
			keys = append(keys, fmt.Sprintf("%d:%s", userID, localDate))
			return nil
		},
	})
	if err := RecoverDueJobs(context.Background(), queue, time.Date(2026, 7, 13, 0, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"46:2026-07-12": true, "47:2026-07-11": true}
	if len(keys) != 2 {
		t.Fatalf("stale summary jobs = %#v", keys)
	}
	for _, key := range keys {
		if !want[key] {
			t.Fatalf("unexpected summary job %q in %#v", key, keys)
		}
	}
}

func seedPublishedDayWithCount(t *testing.T, userID uint, date string, actualCount int) *RecommendationDay {
	t.Helper()
	settings := DefaultRecommendationSettings(userID)
	settings.Enabled = true
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	day, err := CreateRecommendationDay(userID, date, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&RecommendationDay{}).Where("id = ?", day.ID).Updates(map[string]interface{}{
		"status": RecommendationDayStatusPublished, "actual_count": actualCount,
	}).Error; err != nil {
		t.Fatal(err)
	}
	refreshed, err := getRecommendationDay(userID, date)
	if err != nil {
		t.Fatal(err)
	}
	return refreshed
}

func TestEnqueuePublishedDigestSummaryUsesSharedQueue(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC))
	var gotUser uint
	var gotDate string
	stop, err := jobqueue.Start(context.Background(), db, jobqueue.Handlers{
		GenerateDigestSummary: func(_ context.Context, userID uint, localDate string) error {
			gotUser = userID
			gotDate = localDate
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)

	snapshot := &RecommendationDaySnapshot{Day: &RecommendationDay{
		UserID: 77, RecommendationDate: "2026-06-01", Status: RecommendationDayStatusPublished, ActualCount: 2,
	}}
	enqueuePublishedDigestSummary(context.Background(), snapshot)
	if gotUser != 77 || gotDate != "2026-06-01" {
		t.Fatalf("enqueued = %d %q", gotUser, gotDate)
	}

	snapshot.Day.SummaryText = "已有"
	snapshot.Day.SummaryActualCount = 2
	gotUser, gotDate = 0, ""
	enqueuePublishedDigestSummary(context.Background(), snapshot)
	if gotUser != 0 || gotDate != "" {
		t.Fatalf("cached day re-enqueued: %d %q", gotUser, gotDate)
	}
}

func TestGenerateDailyRecommendationsEnqueuesDigestSummary(t *testing.T) {
	setupSQLiteDB(t)
	useRecommendationTestClock(t, time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC))
	settings := DefaultRecommendationSettings(78)
	settings.DailyLimit = 1
	if _, err := SaveRecommendationSettings(&settings); err != nil {
		t.Fatal(err)
	}
	createReadyCandidate(t, "https://digest-job.example/a", "Digest job", []string{"go"}, "digest-job", 0.9, 0.8)

	var gotUser uint
	var gotDate string
	stop, err := jobqueue.Start(context.Background(), db, jobqueue.Handlers{
		GenerateDigestSummary: func(_ context.Context, userID uint, localDate string) error {
			gotUser = userID
			gotDate = localDate
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)

	if _, err := GenerateDailyRecommendations(context.Background(), 78, "2026-06-01"); err != nil {
		t.Fatal(err)
	}
	if gotUser != 78 || gotDate != "2026-06-01" {
		t.Fatalf("publish enqueue = %d %q", gotUser, gotDate)
	}

	gotUser, gotDate = 0, ""
	if _, err := GenerateDailyRecommendations(context.Background(), 78, "2026-06-01"); err != nil {
		t.Fatal(err)
	}
	if gotUser != 78 || gotDate != "2026-06-01" {
		t.Fatalf("stale published day was not re-enqueued: %d %q", gotUser, gotDate)
	}

	if err := db.Model(&RecommendationDay{}).Where("user_id = ? AND recommendation_date = ?", 78, "2026-06-01").
		Updates(map[string]interface{}{"summary_text": "已有", "summary_actual_count": 1}).Error; err != nil {
		t.Fatal(err)
	}
	gotUser, gotDate = 0, ""
	if _, err := GenerateDailyRecommendations(context.Background(), 78, "2026-06-01"); err != nil {
		t.Fatal(err)
	}
	if gotUser != 0 || gotDate != "" {
		t.Fatalf("cached day re-enqueued: %d %q", gotUser, gotDate)
	}
}
