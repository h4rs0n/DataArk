package recommendation

import (
	"DataArk/jobqueue"
	"context"
	"testing"
	"time"
)

func TestRecoverDueJobsEnqueuesOnlyMissingLocalDay(t *testing.T) {
	setupSQLiteDB(t)
	for _, userID := range []uint{41, 42} {
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

	var users []uint
	queue := jobqueue.NewMemoryQueue(jobqueue.NewMemoryStore(), jobqueue.Handlers{
		GenerateDaily: func(_ context.Context, userID uint, localDate string) error {
			if localDate != "2026-07-13" {
				t.Fatalf("local date = %q", localDate)
			}
			users = append(users, userID)
			return nil
		},
	})
	now := time.Date(2026, 7, 13, 0, 30, 0, 0, time.UTC)
	if err := RecoverDueJobs(context.Background(), queue, now); err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0] != 41 {
		t.Fatalf("recovered users = %#v", users)
	}
}
