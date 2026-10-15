package assessment

import (
	"DataArk/jobqueue"
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestBatchHistoryOmitsRolledBackRunAndDetails(t *testing.T) {
	_, _, c := setupBatchFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	run := BatchRun{Status: "rolled_back", CreatedAt: now, FinishedAt: &now}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	batch := Batch{RunID: run.ID, UUID: "returned-history", Phase: "done", CreatedAt: now, FinishedAt: &now, Error: "Batch provider HTTP 401"}
	if err := db.Create(&batch).Error; err != nil {
		t.Fatal(err)
	}
	item := BatchItem{RunID: run.ID, BatchID: &batch.ID, MaterialID: c.MaterialID, ContentVersion: 1, JobID: "old-job", CustomID: "returned-item", Status: "failed", Error: "Batch provider HTTP 401", ReturnedAt: &now, CreatedAt: now}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := ListBatches(ctx, 20, 0)
	if err != nil || len(rows) != 0 {
		t.Fatal("returned batches still listed", rows, err)
	}
	if _, err := GetBatchDetail(ctx, batch.ID, 50, 0); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("returned detail still exposed", err)
	}
	snapshot, err := DecorateQueue(ctx, &jobqueue.CrawlQueueSnapshot{})
	if err != nil || snapshot.BatchRun != nil {
		t.Fatal("returned run still displayed", snapshot, err)
	}
	// Recovery audit stays available in the database.
	var retained BatchItem
	if err := db.First(&retained, item.ID).Error; err != nil || retained.Error != item.Error {
		t.Fatal("audit was removed", retained, err)
	}
	current := BatchRun{Status: "completed", CreatedAt: now, FinishedAt: &now}
	if err := db.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	live := Batch{RunID: current.ID, UUID: "current-history", Phase: "done", CreatedAt: now, FinishedAt: &now}
	if err := db.Create(&live).Error; err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		i := BatchItem{RunID: current.ID, BatchID: &live.ID, MaterialID: c.MaterialID, ContentVersion: 1, JobID: "live-job", CustomID: []string{"live-1", "live-2", "returned-live"}[n], Status: "failed", Error: "HTTP 401", CreatedAt: now}
		if n == 2 {
			i.ReturnedAt = &now
		}
		if err := db.Create(&i).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err = ListBatches(ctx, 20, 0)
	if err != nil || len(rows) != 1 || rows[0].ID != live.ID {
		t.Fatal(rows, err)
	}
	page, err := GetBatchDetail(ctx, live.ID, 1, 0)
	if err != nil || len(page.Items) != 1 || page.NextID == 0 {
		t.Fatal(page, err)
	}
	page2, err := GetBatchDetail(ctx, live.ID, 10, page.NextID)
	if err != nil || len(page2.Items) != 1 || page2.Items[0].ID == page.Items[0].ID {
		t.Fatal("pagination or returned filtering", page2, err)
	}
	snapshot, err = DecorateQueue(ctx, &jobqueue.CrawlQueueSnapshot{})
	if err != nil || snapshot.BatchRun == nil || snapshot.BatchRun.ID != current.ID {
		t.Fatal(snapshot, err)
	}
}
