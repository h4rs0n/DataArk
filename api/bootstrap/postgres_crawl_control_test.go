package bootstrap

import (
	"DataArk/jobqueue"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

// Called by the disposable-database release gate after production migrations.
func verifyPostgresCrawlControl(t *testing.T, database *gorm.DB) {
	ctx := context.Background()
	if err := database.Exec("UPDATE discovery_crawl_settings SET auto_start = false WHERE id = 1").Error; err != nil {
		t.Fatal(err)
	}
	var executions atomic.Int32
	var queue jobqueue.JobEnqueuer
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	handlers := jobqueue.Handlers{FetchSource: func(ctx context.Context, id uint) error {
		executions.Add(1)
		if id == 10001 {
			return queue.EnqueueProcessCandidate(ctx, 10001, "1")
		}
		if id >= 20001 && id <= 20004 {
			started <- struct{}{}
			<-release
		}
		return nil
	}, ProcessCandidate: func(context.Context, uint, string) error { executions.Add(1); return nil }}
	stop, err := jobqueue.Start(ctx, database, handlers, func(ctx context.Context, enqueuer jobqueue.JobEnqueuer) error {
		return enqueuer.EnqueueFetchSource(ctx, 10001)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	defer closeRelease()
	queue, _ = jobqueue.Default()
	controller, _ := jobqueue.CrawlControl()
	snapshot, err := controller.Snapshot(ctx, 50)
	if err != nil || snapshot.Mode != "manual" || !snapshot.CanRun || executions.Load() != 0 {
		t.Fatalf("startup=%#v err=%v executions=%d", snapshot, err, executions.Load())
	}
	if _, err := controller.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(ctx); err != nil {
		t.Fatal(err)
	}
	waitForPostgresCondition(t, 10*time.Second, func() bool {
		s, e := controller.Snapshot(ctx, 50)
		return e == nil && s.State != "running" && executions.Load() == 2
	})
	assertPostgresScalar(t, database, "SELECT (paused_at IS NOT NULL)::text FROM river_queue WHERE name = 'discovery_crawl'", "true")
	if err := queue.EnqueueFetchSource(ctx, 10002); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("UPDATE river_job SET state = 'scheduled', scheduled_at = now() + interval '1 hour' WHERE kind = ? AND args->>'source_id' = '10002'", jobqueue.FetchSourceJobKind).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Run(ctx); err != nil {
		t.Fatal(err)
	}
	waitForPostgresCondition(t, 10*time.Second, func() bool { s, e := controller.Snapshot(ctx, 50); return e == nil && s.State == "waiting" && s.CanRun })
	if executions.Load() != 2 {
		t.Fatal("future job ran in manual round")
	}
	if err := database.Exec("UPDATE river_job SET scheduled_at = now() WHERE kind = ? AND args->>'source_id' = '10002'", jobqueue.FetchSourceJobKind).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := controller.SetAutoStart(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitForPostgresCondition(t, 10*time.Second, func() bool { return executions.Load() == 3 })
	assertPostgresScalar(t, database, "SELECT auto_start::text FROM discovery_crawl_settings WHERE id = 1", "true")
	stop()
	stopAgain, err := jobqueue.Start(ctx, database, handlers, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stopAgain()
	queue, _ = jobqueue.Default()
	controller, _ = jobqueue.CrawlControl()
	snapshot, err = controller.Snapshot(ctx, 50)
	if err != nil || snapshot.Mode != "automatic" {
		t.Fatalf("automatic restart=%#v err=%v", snapshot, err)
	}
	if _, err := controller.SetAutoStart(ctx, false); err != nil {
		t.Fatal(err)
	}
	for id := uint(20001); id <= 20005; id++ {
		if err := queue.EnqueueFetchSource(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := controller.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("blocked workers did not start")
		}
	}
	// A previous manual monitor must not pause a newly enabled automatic queue.
	for _, enabled := range []bool{true, false, true, false} {
		if _, err := controller.SetAutoStart(ctx, enabled); err != nil {
			t.Fatal(err)
		}
	}
	closeRelease()
	waitForPostgresCondition(t, 10*time.Second, func() bool { s, e := controller.Snapshot(ctx, 50); return e == nil && s.Counts.Running == 0 })
	snapshot, _ = controller.Snapshot(ctx, 50)
	if executions.Load() != 7 || snapshot.Mode != "manual" || !snapshot.CanRun || snapshot.Counts.Pending != 1 {
		t.Fatalf("disabled=%#v executions=%d", snapshot, executions.Load())
	}
	stopAgain()
	stopThird, err := jobqueue.Start(ctx, database, handlers, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stopThird()
	controller, _ = jobqueue.CrawlControl()
	snapshot, err = controller.Snapshot(ctx, 50)
	if err != nil || snapshot.Mode != "manual" || snapshot.Counts.Pending != 1 || executions.Load() != 7 {
		t.Fatalf("manual restart=%#v err=%v executions=%d", snapshot, err, executions.Load())
	}
	if _, err := controller.SetAutoStart(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitForPostgresCondition(t, 10*time.Second, func() bool { return executions.Load() == 8 })
	// Wait beyond the old manual monitor's quiet window and prove future jobs run.
	if err := database.Exec("INSERT INTO river_job (state, args, kind, queue, scheduled_at, max_attempts) VALUES ('scheduled', '{\"source_id\":20006}', ?, ?, now() + interval '2 seconds', 1)", jobqueue.FetchSourceJobKind, jobqueue.DiscoveryQueueName).Error; err != nil {
		t.Fatal(err)
	}
	waitForPostgresCondition(t, 10*time.Second, func() bool { return executions.Load() == 9 })
	if _, err := controller.SetAutoStart(ctx, false); err != nil {
		t.Fatal(err)
	}
}
