package recommendation

import (
	"DataArk/jobqueue"
	"context"
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
