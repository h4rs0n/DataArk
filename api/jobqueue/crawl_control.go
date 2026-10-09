package jobqueue

import (
	"context"
	"log"
	"time"
)

func (queue *riverQueue) crawlRun(ctx context.Context) (*CrawlQueueSnapshot, error) {
	queue.crawlMu.Lock()
	defer queue.crawlMu.Unlock()
	if queue.crawlAutoStart || queue.crawlRunning {
		return queue.crawlSnapshotLocked(ctx, 50)
	}
	if err := queue.client.QueueResume(ctx, DiscoveryQueueName, nil); err != nil {
		return nil, err
	}
	queue.crawlRunning = true
	queue.crawlStartedAt = time.Now()
	queue.crawlGeneration++
	go queue.pauseCrawlWhenDrained(queue.crawlGeneration)
	return queue.crawlSnapshotLocked(ctx, 50)
}

func (queue *riverQueue) setCrawlAutoStart(ctx context.Context, enabled bool) (*CrawlQueueSnapshot, error) {
	queue.crawlMu.Lock()
	defer queue.crawlMu.Unlock()
	tx, err := queue.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE discovery_crawl_settings SET auto_start = $1 WHERE id = 1", enabled); err != nil {
		return nil, err
	}
	if enabled {
		err = queue.client.QueueResumeTx(ctx, tx, DiscoveryQueueName, nil)
	} else {
		err = queue.client.QueuePauseTx(ctx, tx, DiscoveryQueueName, nil)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	queue.crawlAutoStart = enabled
	queue.crawlRunning = false
	queue.crawlStartedAt = time.Time{}
	queue.crawlGeneration++
	// riverdatabasesql has no LISTEN support. Its settings poll can miss a
	// pause/resume cycle between polls, so also notify this client's producers
	// through the non-transactional API after committing the durable state.
	// Finish synchronization even if the HTTP request was cancelled at commit.
	syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if enabled {
		err = queue.client.QueueResume(syncCtx, DiscoveryQueueName, nil)
	} else {
		err = queue.client.QueuePause(syncCtx, DiscoveryQueueName, nil)
	}
	if err != nil {
		return nil, err
	}
	return queue.crawlSnapshotLocked(ctx, 50)
}

func (queue *riverQueue) pauseCrawlWhenDrained(generation uint64) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	quietChecks := 0
	for {
		select {
		case <-queue.stop:
			return
		case <-ticker.C:
			queue.crawlMu.Lock()
			if queue.crawlGeneration != generation || queue.crawlAutoStart || !queue.crawlRunning {
				queue.crawlMu.Unlock()
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			counted, err := countRiverQueueJobs(ctx, stdSQLQuerier{db: queue.db}, DiscoveryQueueName, discoveryJobKinds, time.Now())
			if err != nil || counted.Runnable > 0 {
				quietChecks = 0
			} else {
				quietChecks++
				if quietChecks >= 2 {
					err = queue.client.QueuePause(ctx, DiscoveryQueueName, nil)
					if err == nil {
						queue.crawlRunning = false
						queue.crawlStartedAt = time.Time{}
					} else {
						log.Printf("manual crawl queue pause failed: %v", err)
						quietChecks = 0
					}
				}
			}
			cancel()
			finished := !queue.crawlRunning
			queue.crawlMu.Unlock()
			if finished {
				return
			}
		}
	}
}
