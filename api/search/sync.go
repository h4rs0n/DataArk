package search

import (
	"DataArk/config"
	"DataArk/database"
	"DataArk/material"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
)

const syncBatchSize = 100
const syncBackfillBatchSize = 1000
const syncIntegrityCheckInterval = 5 * time.Minute
const syncLock = 48487623048
const generationMarker = "_search_generation"

type SyncState struct {
	ID            int        `json:"-"`
	Phase         string     `json:"phase"`
	Generation    string     `json:"-"`
	Cursor        uint       `json:"cursor"`
	Scanned       int64      `json:"scanned"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	Error         string     `json:"error"`
}

func (SyncState) TableName() string { return "search_sync_state" }

type syncPending struct {
	MaterialID    uint `gorm:"primaryKey"`
	Revision      int64
	Attempts      int
	NextAttemptAt time.Time
	Error         string
}

func (syncPending) TableName() string { return "search_sync_pending" }

type SyncStatus struct {
	SyncState
	Pending int64 `json:"pending"`
	Failed  int64 `json:"failed"`
}

func GetSyncStatus(ctx context.Context) (*SyncStatus, error) {
	db := database.DB()
	if db == nil {
		return nil, errors.New("search database unavailable")
	}
	result := &SyncStatus{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&result.SyncState, 1).Error; err != nil {
			return err
		}
		if err := tx.Model(&syncPending{}).Count(&result.Pending).Error; err != nil {
			return err
		}
		return tx.Model(&syncPending{}).Where("attempts > 0").Count(&result.Failed).Error
	})
	return result, err
}

// StartSync owns an independent, restartable worker. No LLM work happens here.
func StartSync(parent context.Context) func() {
	db := database.DB()
	if db == nil || db.Dialector.Name() != "postgres" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner := &syncRunner{}
		delay := time.Duration(0)
		for {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			worked := false
			err := withSyncLock(ctx, db, func(tx *gorm.DB) error {
				var err error
				worked, err = runner.step(ctx, tx)
				if err != nil {
					tx.Model(&SyncState{}).Where("id = 1").Update("error", err.Error())
				}
				return err
			})
			delay = 2 * time.Second
			if err != nil {
				log.Printf("search sync: %v", err)
				delay = 5 * time.Second
			} else if worked {
				delay = 0
			}
		}
	}()
	return func() { cancel(); <-done }
}

// Use a dedicated connection, and release the session lock even on cancellation.
func withSyncLock(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error) error {
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		conn = conn.Session(&gorm.Session{NewDB: true})
		var locked bool
		if err := conn.Raw("SELECT pg_try_advisory_lock(?)", syncLock).Scan(&locked).Error; err != nil {
			return err
		}
		if !locked {
			return nil
		}
		defer conn.WithContext(context.Background()).Exec("SELECT pg_advisory_unlock(?)", syncLock)
		return fn(conn)
	})
}

func decodeKeywords(raw string, target *[]string) { _ = json.Unmarshal([]byte(raw), target) }
func searchClient() meilisearch.ServiceManager {
	return meilisearch.New(config.MEILIHOST, meilisearch.WithAPIKey(config.MEILIAPIKey))
}
func stageIndex(s SyncState) string { return config.MEILIBlogsIndex + "_build_" + s.Generation }
func saveState(db *gorm.DB, values map[string]interface{}) error {
	return db.Model(&SyncState{}).Where("id = 1").Updates(values).Error
}

func syncStep(ctx context.Context, db *gorm.DB) (bool, error) {
	return (&syncRunner{}).step(ctx, db)
}

// Each worker owns its check schedule. A restart checks immediately; no shared
// cache can suppress validation for a different index or a new generation.
type syncRunner struct {
	nextIntegrityCheck time.Time
	checkedGeneration  string
	now                func() time.Time
}

func (runner *syncRunner) step(ctx context.Context, db *gorm.DB) (bool, error) {
	var s SyncState
	if err := db.First(&s, 1).Error; err != nil {
		return false, err
	}
	if s.Phase != "ready" {
		runner.nextIntegrityCheck = time.Time{}
		runner.checkedGeneration = ""
	}
	client := searchClient()
	switch s.Phase {
	case "prepare":
		if s.Generation == "" {
			s.Generation = uuid.NewString()
			if err := saveState(db, map[string]interface{}{"generation": s.Generation}); err != nil {
				return false, err
			}
		}
		index := stageIndex(s)
		if _, err := client.GetIndexWithContext(ctx, index); err != nil {
			if !meiliMissing(err) {
				return false, err
			}
			task, err := client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{Uid: index, PrimaryKey: "id"})
			if err != nil {
				return false, err
			}
			if err := waitSearchTask(ctx, client, task); err != nil {
				return false, err
			}
		}
		attrs := []string{"title", "summary", "keywords", "content"}
		task, err := client.Index(index).UpdateSearchableAttributesWithContext(ctx, &attrs)
		if err != nil {
			return false, err
		}
		if err := waitSearchTask(ctx, client, task); err != nil {
			return false, err
		}
		task, err = client.Index(index).AddDocumentsWithContext(ctx, []map[string]string{{"id": generationMarker, "generation": s.Generation}}, nil)
		if err != nil {
			return false, err
		}
		if err := waitSearchTask(ctx, client, task); err != nil {
			return false, err
		}
		return true, saveState(db, map[string]interface{}{"phase": "scan", "error": ""})
	case "scan":
		var ids []uint
		if err := db.Model(&material.Material{}).Where("id > ?", s.Cursor).Order("id").Limit(syncBackfillBatchSize).Pluck("id", &ids).Error; err != nil {
			return false, err
		}
		if len(ids) == 0 {
			return true, saveState(db, map[string]interface{}{"phase": "catchup"})
		}
		if err := syncMaterials(ctx, db, client, stageIndex(s), ids); err != nil {
			return false, err
		}

		return true, saveState(db, map[string]interface{}{"cursor": ids[len(ids)-1], "scanned": s.Scanned + int64(len(ids)), "error": ""})
	case "catchup", "ready":
		target := config.MEILIBlogsIndex
		if s.Phase == "catchup" {
			target = stageIndex(s)
		}
		var pending []syncPending
		batchSize := syncBatchSize
		if s.Phase == "catchup" {
			batchSize = syncBackfillBatchSize
		}
		if err := db.Where("next_attempt_at <= ?", time.Now()).Order("next_attempt_at, material_id").Limit(batchSize).Find(&pending).Error; err != nil {
			return false, err
		}
		if s.Phase == "catchup" {
			if _, err := client.GetIndexWithContext(ctx, target); err != nil {
				if meiliMissing(err) {
					return true, resetSync(db)
				}
				return false, err
			}
		} else {
			now := time.Now()
			if runner.now != nil {
				now = runner.now()
			}
			if runner.checkedGeneration != s.Generation || !now.Before(runner.nextIntegrityCheck) {
				// The marker request alone detects a missing index as well as an
				// empty or replaced one; a separate GetIndex request is redundant.
				var marker map[string]interface{}
				err := client.Index(target).GetDocumentWithContext(ctx, generationMarker, nil, &marker)
				if err != nil && !meiliMissing(err) {
					return false, err
				}
				if marker["generation"] != s.Generation {
					return true, resetSync(db)
				}
				runner.checkedGeneration = s.Generation
				runner.nextIntegrityCheck = now.Add(syncIntegrityCheckInterval)
			}
		}
		ids := make([]uint, 0, len(pending))
		for _, item := range pending {
			ids = append(ids, item.MaterialID)
		}
		batchErr := syncMaterials(ctx, db, client, target, ids)
		if meiliMissing(batchErr) {
			// A write can discover index loss before the periodic check. Keep
			// the pending rows and begin recovery immediately in that case.
			return true, resetSync(db)
		}

		for _, item := range pending {
			if err := batchErr; err != nil {
				delay := time.Second * time.Duration(1<<min(item.Attempts+1, 8))
				if delay > 5*time.Minute {
					delay = 5 * time.Minute
				}
				if e := db.Model(&syncPending{}).Where("material_id = ? AND revision = ?", item.MaterialID, item.Revision).Updates(map[string]interface{}{"attempts": item.Attempts + 1, "next_attempt_at": time.Now().Add(delay), "error": err.Error()}).Error; e != nil {
					return false, e
				}
				log.Printf("search sync material %d: %v", item.MaterialID, err)
				continue
			}
			// A trigger incrementing revision during indexing leaves the new work intact.
			if err := db.Where("material_id = ? AND revision = ?", item.MaterialID, item.Revision).Delete(&syncPending{}).Error; err != nil {
				return false, err
			}
			if err := saveState(db, map[string]interface{}{"last_success_at": time.Now(), "error": ""}); err != nil {
				return false, err
			}
		}
		if s.Phase == "catchup" {
			var remaining int64
			if err := db.Model(&syncPending{}).Count(&remaining).Error; err != nil {
				return false, err
			}
			if remaining == 0 {
				return true, saveState(db, map[string]interface{}{"phase": "swap"})
			}
		}
		if len(pending) == 0 && s.Error != "" {
			return false, saveState(db, map[string]interface{}{"error": ""})
		}
		return len(pending) > 0, nil
	case "swap":
		return true, swapSearchIndex(ctx, db, client, s)
	default:
		return false, fmt.Errorf("unknown search sync phase %q", s.Phase)
	}
}

func resetSync(db *gorm.DB) error {
	return saveState(db, map[string]interface{}{"phase": "prepare", "generation": "", "cursor": 0, "scanned": 0, "error": ""})
}

func syncMaterials(ctx context.Context, db *gorm.DB, client meilisearch.ServiceManager, index string, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	documents := make([]Document, 0, len(ids))
	deleted := make([]string, 0, len(ids))
	// Read body, assessment and archives from one consistent snapshot.
	err := db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ").Error; err != nil {
				return err
			}
		}
		for _, id := range ids {
			doc, err := buildMaterialDocument(tx, id)
			if err != nil {
				return err
			}
			if doc == nil {
				deleted = append(deleted, documentID(id))
			} else {
				documents = append(documents, *doc)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(documents) > 0 {
		task, err := client.Index(index).AddDocumentsWithContext(ctx, documents, nil)
		if err != nil {
			return err
		}
		if err := waitSearchTask(ctx, client, task); err != nil {
			return err
		}
	}
	if len(deleted) > 0 {
		task, err := client.Index(index).DeleteDocumentsWithContext(ctx, deleted, nil)
		if err != nil {
			return err
		}
		return waitSearchTask(ctx, client, task)
	}
	return nil
}

func waitSearchTask(ctx context.Context, client meilisearch.ServiceManager, info *meilisearch.TaskInfo) error {
	if info == nil {
		return errors.New("empty Meilisearch task")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	task, err := client.WaitForTaskWithContext(waitCtx, info.TaskUID, 100*time.Millisecond)
	if err != nil {
		return err
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return fmt.Errorf("Meilisearch task %d: %s: %v", info.TaskUID, task.Status, task.Error)
	}
	return nil
}

func meiliMissing(err error) bool {
	var apiErr *meilisearch.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == 404
}

func swapSearchIndex(ctx context.Context, db *gorm.DB, client meilisearch.ServiceManager, s SyncState) error {
	// Recover the response-loss window by inspecting swap history for this unique
	// staging index before ever submitting another swap (a swap is not idempotent).
	tasks, err := client.GetTasksWithContext(ctx, &meilisearch.TasksQuery{IndexUIDS: []string{stageIndex(s)}, Types: []meilisearch.TaskType{"indexSwap"}, Limit: 1})
	if err != nil {
		return err
	}
	completed := false
	if len(tasks.Results) > 0 {
		task := tasks.Results[0]
		if task.Status == meilisearch.TaskStatusEnqueued || task.Status == meilisearch.TaskStatusProcessing {
			if err := waitSearchTask(ctx, client, &meilisearch.TaskInfo{TaskUID: task.UID}); err != nil {
				return err
			}
			completed = true
		} else {
			completed = task.Status == meilisearch.TaskStatusSucceeded
		}
	}
	if !completed {
		// A successfully swapped marker also protects against pruned task history.
		var marker map[string]interface{}
		err := client.Index(config.MEILIBlogsIndex).GetDocumentWithContext(ctx, generationMarker, nil, &marker)
		if err != nil && !meiliMissing(err) {
			return err
		}
		completed = marker["generation"] == s.Generation
	}
	if !completed {
		if _, err := client.GetIndexWithContext(ctx, config.MEILIBlogsIndex); err != nil {
			if !meiliMissing(err) {
				return err
			}
			task, err := client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{Uid: config.MEILIBlogsIndex, PrimaryKey: "id"})
			if err != nil {
				return err
			}
			if err := waitSearchTask(ctx, client, task); err != nil {
				return err
			}
		}
		task, err := client.SwapIndexesWithContext(ctx, []*meilisearch.SwapIndexesParams{{Indexes: []string{config.MEILIBlogsIndex, stageIndex(s)}}})
		if err != nil {
			return err
		}
		if err := waitSearchTask(ctx, client, task); err != nil {
			return err
		}
	}
	if err := saveState(db, map[string]interface{}{"phase": "ready", "last_success_at": time.Now(), "error": ""}); err != nil {
		return err
	}
	// Old index cleanup is safe only after the durable state says ready.
	if task, err := client.DeleteIndexWithContext(ctx, stageIndex(s)); err == nil {
		_ = waitSearchTask(ctx, client, task)
	}
	return nil
}

// Maintenance and restore rebuild exactly the same projection as startup sync.
func rebuildMaterialIndex(ctx context.Context, db *gorm.DB, skipInvalid bool) (*RebuildIndexResult, []ArchiveConsistencyIssue, error) {
	var result *RebuildIndexResult
	var issues []ArchiveConsistencyIssue
	ran := false
	err := withSyncLock(ctx, db, func(conn *gorm.DB) error {
		ran = true
		// The scanner reconciles physical HTML files into archive/material metadata.
		_, _, found, err := scanArchiveHTMLFiles(config.ARCHIVEFILELOACTION)
		issues = found
		if err != nil {
			return err
		}
		if !skipInvalid && len(issues) > 0 {
			return fmt.Errorf("archive reconciliation: %s", issues[0].Message)
		}
		if err := resetSync(conn); err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := syncStep(ctx, conn); err != nil {
				return err
			}
			var state SyncState
			if err := conn.First(&state, 1).Error; err != nil {
				return err
			}
			if state.Phase == "ready" {
				stats, err := searchClient().Index(config.MEILIBlogsIndex).GetStatsWithContext(ctx, nil)
				if err != nil {
					return err
				}
				result = &RebuildIndexResult{Documents: max(0, int(stats.NumberOfDocuments)-1)}
				return nil
			}
			if state.Phase == "catchup" {
				timer := time.NewTimer(100 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
	})
	if err == nil && !ran {
		err = errors.New("搜索索引同步正在运行，请稍后重试")
	}
	return result, issues, err
}
