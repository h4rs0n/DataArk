package search

import (
	"DataArk/archive"
	"DataArk/database"
	"DataArk/material"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"DataArk/config"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Requires an explicitly named disposable database and isolated Meilisearch.
func TestPostgresSearchSync(t *testing.T) {
	dsn, host := os.Getenv("DATAARK_SEARCH_TEST_DSN"), os.Getenv("DATAARK_SEARCH_TEST_MEILI")
	if dsn == "" || host == "" {
		t.Skip("set DATAARK_SEARCH_TEST_DSN and DATAARK_SEARCH_TEST_MEILI for isolated integration tests")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil || !strings.HasPrefix(name, "dataark_search_verify") {
		t.Fatalf("refusing non-disposable database %q: %v", name, err)
	}
	if err := database.RunDatabaseMigrations(db); err != nil {
		t.Fatal(err)
	}
	// This test exclusively owns its isolated DB; re-runs start from clean fixtures.
	if err := db.Exec("TRUNCATE material, archive_documents, archive_tasks, search_sync_pending CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	oldDB, oldArchive := database.SetDB(db), archive.SetDB(db)
	oldHost, oldKey, oldIndex, oldRoot := config.MEILIHOST, config.MEILIAPIKey, config.MEILIBlogsIndex, config.ARCHIVEFILELOACTION
	config.MEILIHOST, config.MEILIAPIKey, config.MEILIBlogsIndex, config.ARCHIVEFILELOACTION = host, "", "search_verify_"+fmt.Sprint(time.Now().UnixNano()), t.TempDir()
	defer func() {
		database.SetDB(oldDB)
		archive.SetDB(oldArchive)
		config.MEILIHOST, config.MEILIAPIKey, config.MEILIBlogsIndex, config.ARCHIVEFILELOACTION = oldHost, oldKey, oldIndex, oldRoot
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(resetSync(db))
	create := func(url, title, body string, assessed bool) material.Material {
		t.Helper()
		var core material.Material
		must(db.Transaction(func(tx *gorm.DB) error {
			var err error
			core, err = material.EnsureURL(tx, url, title, "summary signal")
			if err != nil {
				return err
			}
			version, err := material.AppendText(tx, core, body, len(strings.Fields(body)), "fixture", time.Now())
			if err != nil {
				return err
			}
			values := map[string]interface{}{"content_version": version.Version}
			if assessed {
				var id uint
				if err := tx.Raw(`INSERT INTO discovery_article_assessments(material_id,material_version_id,content_version,assessor,assessor_version,policy_version,overall_quality,summary,keywords) VALUES(?,?,?,'test_llm','v1','v1',0.01,'abstractneedle 摘要','["keywordneedle"]') RETURNING id`, core.ID, version.ID, version.Version).Scan(&id).Error; err != nil {
					return err
				}
				values["assessment_state"], values["current_assessment_id"], values["eligibility_state"] = "ready", id, "ineligible"
			}
			return tx.Model(&material.ArticleState{}).Where("material_id = ?", core.ID).Updates(values).Error
		}))
		return core
	}
	low := create("https://example.org/search-low", "titleneedle 标题", "bodyneedle 完整正文 <img src=x onerror=alert(1)>", true)
	pending := create("https://example.org/pending", "pendingneedle", "pendingbody", false)
	drain := func() {
		t.Helper()
		for i := 0; i < 50; i++ {
			_, err := syncStep(ctx, db)
			must(err)
			var state SyncState
			must(db.First(&state, 1).Error)
			var n int64
			must(db.Model(&syncPending{}).Count(&n).Error)
			if state.Phase == "ready" && n == 0 {
				return
			}
		}
		t.Fatal("sync failed to drain")
	}
	drain()
	query := func(q string) []Result {
		t.Helper()
		raw, _ := QueryByKeyword(q, 1)
		if raw == "Error" {
			t.Fatal("query failed")
		}
		var hits []Result
		must(json.Unmarshal([]byte(raw), &hits))
		return hits
	}
	for _, q := range []string{"titleneedle", "abstractneedle", "keywordneedle", "bodyneedle"} {
		if hits := query(q); len(hits) != 1 || hits[0].MaterialID != low.ID || hits[0].Archived {
			t.Fatalf("query %s: %+v", q, hits)
		}
	}
	must(db.Exec("INSERT INTO discovery_domain_blacklist_entries(domain) VALUES('example.org') ON CONFLICT(domain) DO NOTHING").Error)
	if len(query("bodyneedle")) != 1 {
		t.Fatal("discovery blacklist affected search")
	}
	if len(query("pendingneedle")) != 0 {
		t.Fatal("unassessed article leaked")
	}
	hits := query("bodyneedle")
	if strings.Contains(hits[0].Content, "<img") || !strings.Contains(hits[0].Content, "search-highlight") {
		t.Fatal("unsafe or unhighlighted snippet", hits)
	}
	// Capturing the same material must update the one stable document.
	path := filepath.Join(config.ARCHIVEFILELOACTION, "example.org", "saved #1.html")
	must(os.MkdirAll(filepath.Dir(path), 0755))
	must(os.WriteFile(path, []byte("<html><title>Saved</title><body>archivedbody</body></html>"), 0644))
	must(archive.SaveArchiveDocumentDetails("example.org", "saved #1.html", "https://example.org/search-low", "Saved", "saved summary"))
	drain()
	hits = query("bodyneedle")
	if len(hits) != 1 || !hits[0].Archived || !strings.Contains(hits[0].ArchivePath, "%23") {
		t.Fatalf("archive hydration: %+v", hits)
	}
	secondPath := filepath.Join(config.ARCHIVEFILELOACTION, "example.org", "second.html")
	must(os.WriteFile(secondPath, []byte("<html><title>Second</title><body>second saved body</body></html>"), 0644))
	must(archive.SaveArchiveDocumentDetails("example.org", "second.html", "https://example.org/search-low", "Second", "second"))
	drain()
	if got := query("bodyneedle"); len(got) != 1 || got[0].Filename != "second.html" {
		t.Fatalf("multiple archives were not merged: %+v", got)
	}
	locations, err := (meiliArchiveIndexStore{}).ListArchiveDocuments(ctx)
	must(err)
	if len(locations) != 2 || locations[0].ID != locations[1].ID {
		t.Fatalf("archive aliases missing: %+v", locations)
	}
	must(os.Remove(secondPath))
	must(archive.DeleteArchiveDocumentMetadata("example.org", "second.html"))
	drain()
	// A successful capture is reusable only while its local file still exists.
	originalEnsure, originalEnqueue := ensureArchiveTaskQueue, enqueueArchiveTask
	ensureArchiveTaskQueue = func() error { return nil }
	enqueueArchiveTask = func(string) {}
	defer func() { ensureArchiveTaskQueue, enqueueArchiveTask = originalEnsure, originalEnqueue }()
	oldTask := &archive.ArchiveTask{ID: "successful-capture", URL: "https://example.org/search-low", Domain: "example.org", FileName: "saved #1.html", Status: ArchiveTaskStatusSuccess}
	must(archive.CreateArchiveTask(oldTask))
	reused, created, err := AddDocURLTask(oldTask.URL)
	must(err)
	if created || reused.ID != oldTask.ID {
		t.Fatal("existing file was not reused")
	}
	// A missing physical file falls back to online immediately, even before sync.
	must(os.Remove(path))
	if query("bodyneedle")[0].Archived {
		t.Fatal("missing file still local")
	}
	must(archive.DeleteArchiveDocumentMetadata("example.org", "saved #1.html"))
	drain()
	if len(query("bodyneedle")) != 1 {
		t.Fatal("deletion removed assessed article")
	}
	requeued, created, err := AddDocURLTask(oldTask.URL)
	must(err)
	if !created || requeued.ID == oldTask.ID {
		t.Fatal("deleted archive could not be captured again")
	}
	reused, created, err = AddDocURLTask(oldTask.URL)
	must(err)
	if created || reused.ID != requeued.ID {
		t.Fatal("active capture duplicated")
	}
	// A changed body cannot reuse a historical assessment.
	must(db.Transaction(func(tx *gorm.DB) error {
		v, err := material.AppendText(tx, low, "newversionneedle", 1, "fixture", time.Now())
		if err != nil {
			return err
		}
		return tx.Model(&material.ArticleState{}).Where("material_id = ?", low.ID).Update("content_version", v.Version).Error
	}))
	drain()
	if len(query("bodyneedle")) != 0 || len(query("newversionneedle")) != 0 {
		t.Fatal("historical assessment qualified changed body")
	}
	// Rollback and duplicate pending revisions are transactionally safe.
	var before syncPending
	must(db.Model(&material.Material{}).Where("id = ?", pending.ID).Update("title", "pending two").Error)
	must(db.First(&before, "material_id = ?", pending.ID).Error)
	_ = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&material.Material{}).Where("id = ?", pending.ID).Update("title", "rolled back").Error; err != nil {
			return err
		}
		return fmt.Errorf("rollback")
	})
	var after syncPending
	must(db.First(&after, "material_id = ?", pending.ID).Error)
	if before.Revision != after.Revision {
		t.Fatal("rollback leaked queue change")
	}
	must(db.Model(&material.Material{}).Where("id = ?", pending.ID).Update("title", "pending three").Error)
	must(db.Where("material_id = ? AND revision = ?", before.MaterialID, before.Revision).Delete(&syncPending{}).Error)
	var remaining int64
	must(db.Model(&syncPending{}).Where("material_id = ?", pending.ID).Count(&remaining).Error)
	if remaining != 1 {
		t.Fatal("stale acknowledgement lost update")
	}
	// Index write failures are durable and recover without any assessment work.
	oldClientHost := config.MEILIHOST
	config.MEILIHOST = "http://127.0.0.1:1"
	_, err = syncStep(ctx, db)
	if err == nil {
		t.Fatal("outage should fail")
	}
	config.MEILIHOST = oldClientHost
	drain()
	// An accepted Meilisearch task can still fail asynchronously. Keep it retryable.
	must(db.Model(&material.Material{}).Where("id = ?", pending.ID).Update("title", "retry pending").Error)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/tasks/"):
			_, _ = w.Write([]byte(`{"uid":1,"status":"failed","error":{"message":"fixture index failure","code":"internal","type":"internal","link":""}}`))
		case r.Method == http.MethodDelete || strings.HasSuffix(r.URL.Path, "delete-batch"):
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"taskUid":1,"status":"enqueued"}`))
		default:
			if strings.Contains(r.URL.Path, generationMarker) {
				var state SyncState
				_ = db.First(&state, 1).Error
				_ = json.NewEncoder(w).Encode(map[string]string{"generation": state.Generation})
			} else {
				_, _ = w.Write([]byte(`{"uid":"fixture"}`))
			}
		}
	}))
	config.MEILIHOST = failing.URL
	_, err = syncStep(ctx, db)
	must(err)
	config.MEILIHOST = oldClientHost
	failing.Close()
	var failed syncPending
	must(db.First(&failed, "material_id = ?", pending.ID).Error)
	if failed.Attempts != 1 || !strings.Contains(failed.Error, "fixture index failure") {
		t.Fatalf("failure not durable: %+v", failed)
	}
	must(db.Model(&syncPending{}).Where("material_id = ?", pending.ID).Update("next_attempt_at", time.Now()).Error)
	drain()
	// Re-entering swap after a completed swap must not swap the old index back.
	var state SyncState
	must(db.First(&state, 1).Error)
	must(saveState(db, map[string]interface{}{"phase": "swap"}))
	_, err = syncStep(ctx, db)
	must(err)
	var marker map[string]interface{}
	must(searchClient().Index(config.MEILIBlogsIndex).GetDocumentWithContext(ctx, generationMarker, nil, &marker))
	if marker["generation"] != state.Generation {
		t.Fatal("swap recovery reverted active index")
	}
	// A complete rebuild and external-only consistency check retain assessed works.
	again := create("https://example.org/again", "rebuildneedle", "rebuildbody", true)
	_, _, err = rebuildMaterialIndex(ctx, db, true)
	must(err)
	if len(query("rebuildneedle")) != 1 {
		t.Fatal("rebuild lost assessed article")
	}
	documents, err := (meiliArchiveIndexStore{}).ListArchiveDocuments(ctx)
	must(err)
	if len(documents) != 0 {
		t.Fatal("external documents counted as local", documents)
	}
	// Strong identity merging removes the retired material document.
	other := create("https://example.org/other", "otherneedle", "otherbody", true)
	drain()
	must(db.Transaction(func(tx *gorm.DB) error { _, err := material.Merge(tx, again.ID, other.ID); return err }))
	drain()
	var deleted map[string]interface{}
	err = searchClient().Index(config.MEILIBlogsIndex).GetDocumentWithContext(ctx, documentID(other.ID), nil, &deleted)
	if !meiliMissing(err) {
		t.Fatalf("retired identity remained: %v", err)
	}
	// The independently scheduled worker resumes durable work after a restart.
	stop := StartSync(ctx)
	create("https://example.org/worker-one", "workeroneneedle", "worker first body", true)
	waitHit := func(q string) {
		t.Helper()
		for i := 0; i < 100; i++ {
			if len(query(q)) == 1 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("background sync did not resume", q)
	}
	waitHit("workeroneneedle")
	stop()
	create("https://example.org/worker-two", "workertwoneedle", "worker second body", true)
	stop = StartSync(ctx)
	waitHit("workertwoneedle")
	stop()
	// Even if startup recreated an empty index, the marker forces a full repair.
	task, err := searchClient().DeleteIndexWithContext(ctx, config.MEILIBlogsIndex)
	must(err)
	must(waitSearchTask(ctx, searchClient(), task))
	task, err = searchClient().CreateIndexWithContext(ctx, &meilisearch.IndexConfig{Uid: config.MEILIBlogsIndex, PrimaryKey: "id"})
	must(err)
	must(waitSearchTask(ctx, searchClient(), task))
	drain()
	if len(query("workertwoneedle")) != 1 {
		t.Fatal("empty existing index was not repaired")
	}
	// Clean up only indexes owned by this test.
	task, err = searchClient().DeleteIndexWithContext(ctx, config.MEILIBlogsIndex)
	must(err)
	must(waitSearchTask(ctx, searchClient(), task))
}
