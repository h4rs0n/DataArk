package discovery

import (
	appdatabase "DataArk/database"
	"DataArk/dedupe"
	"DataArk/jobqueue"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Run against an empty disposable database. This exercises production Goose
// migrations, River cancellation, and a 95k-member query plan with real stats.
func TestPostgresDedupeRecoveryAndQueryPlans(t *testing.T) {
	dsn := os.Getenv("DATAARK_DEDUPE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_DEDUPE_POSTGRES_TEST_DSN to an empty disposable database")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	var name string
	if err := database.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "dataark_v3_verify") {
		t.Fatalf("refusing destructive verification against %q", name)
	}
	if err := appdatabase.RunDatabaseMigrations(database); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.Table("discovery_candidates").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("requires empty disposable database", err)
	}
	previous := SetDB(database)
	t.Cleanup(func() { SetDB(previous) })
	first := newIndexedCandidate(t, "pg-representative", dedupeAutoBody())
	second := newIndexedCandidate(t, "pg-covered", dedupeAutoBody()+" note")
	blocked := newIndexedCandidate(t, "pg-blocked", "blocked independent content")
	run := activateRecoveryFixture(t, first, second, blocked)
	var representative dedupe.Member
	if err := database.First(&representative, "run_id=? AND material_id=?", run.ID, first.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&dedupe.Member{}).Where("run_id=? AND material_id=?", run.ID, second.MaterialID).Update("representative_id", representative.RepresentationID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&DiscoveryCandidate{}).Where("id IN ?", []uint{first.ID, second.ID, blocked.ID}).Update("dedupe_state", "ready").Error; err != nil {
		t.Fatal(err)
	}
	mutation, err := CreateDiscoveryDomainBlacklist("pg-blocked.example", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&dedupe.Member{}).Where("run_id=? AND material_id=?", run.ID, blocked.MaterialID).Update("applied", false).Error; err != nil {
		t.Fatal(err)
	}
	if drift, err := dedupeRecoveryDrift(context.Background(), run.ID); err != nil || drift != 0 {
		t.Fatalf("blocked drift=%d err=%v", drift, err)
	}
	stop, err := jobqueue.Start(context.Background(), database, jobqueue.Handlers{
		AssessArticle: func(context.Context, uint, string) error { t.Error("paused assessment executed"); return nil },
		Dedupe:        RunDedupeTask,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	queue, _ := jobqueue.Default()
	targets := []jobqueue.AssessmentTarget{{MaterialID: first.MaterialID, ContentVersion: "1"}, {MaterialID: blocked.MaterialID, ContentVersion: "1"}}
	for i := 0; i < 501; i++ {
		targets = append(targets, jobqueue.AssessmentTarget{MaterialID: second.MaterialID, ContentVersion: fmt.Sprint(i)})
	}
	if err := queue.EnqueueAssessArticles(context.Background(), targets); err != nil {
		t.Fatal(err)
	}
	checks, scanned := 0, 0
	cancelled, err := queue.(jobqueue.AssessmentPruner).PruneAssessments(context.Background(), func(targets []jobqueue.AssessmentTarget) (map[uint]bool, error) {
		checks++
		scanned += len(targets)
		return suppressedDedupeAssessmentTargets(database, run.ID, targets)
	})
	if err != nil || cancelled != 501 || checks != 3 || scanned != 503 {
		t.Fatalf("cancelled=%d batches=%d scanned=%d err=%v", cancelled, checks, scanned, err)
	}
	if err := database.Table("river_job").Where("kind=? AND state='available'", jobqueue.AssessArticleJobKind).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("qualified/blocked assessments were cancelled: remaining=%d err=%v", count, err)
	}
	if err := database.Table("river_job").Where("kind=? AND state='cancelled' AND metadata @> '{\"dedupe_superseded\":true}'", jobqueue.AssessArticleJobKind).Count(&count).Error; err != nil || count != 501 {
		t.Fatalf("River cancellation metadata missing: count=%d err=%v", count, err)
	}
	for _, full := range []bool{false, false, true} {
		if work, err := recoverDedupeJobs(context.Background(), full); err != nil || work != 0 {
			t.Fatalf("blocked recovery did not converge: work=%d err=%v", work, err)
		}
	}
	if err := database.Table("river_job").Where("kind=?", jobqueue.DedupeJobKind).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("blocked member was repeatedly queued: jobs=%d err=%v", count, err)
	}
	if _, err := DeleteDiscoveryDomainBlacklist(mutation.Entry.ID); err != nil {
		t.Fatal(err)
	}
	if err := RecoverDedupeJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		q, err := ArticleDedupeQualification(database, blocked.MaterialID)
		if err != nil {
			t.Fatal(err)
		}
		if q.Ready && q.Representative {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unblocked article did not recover through River")
		}
		time.Sleep(20 * time.Millisecond)
	}
	verifyDedupeQueryPlans(t, database, run.ID)
}

func verifyDedupeQueryPlans(t *testing.T, database *gorm.DB, runID uint) {
	t.Helper()
	const size = 95000
	statements := []string{
		`INSERT INTO material(id,title) SELECT g,'plan fixture' FROM generate_series(1000001,1095000) g`,
		`INSERT INTO material_versions(id,material_id,version) SELECT g,g,1 FROM generate_series(1000001,1095000) g`,
		`UPDATE material SET current_version_id=id WHERE id>1000000`,
		`INSERT INTO material_representations(id,version_id,kind,role,text,content_hash) SELECT g,g,'text','body',repeat('plan fixture content ',100),g::text FROM generate_series(1000001,1095000) g`,
		`INSERT INTO material_article_states(material_id,content_version) SELECT g,1 FROM generate_series(1000001,1095000) g`,
		`INSERT INTO discovery_candidates(id,material_id,url,normalized_url,dedupe_key,crawl_host,processing_state,dedupe_state,status) SELECT g,g,'https://plan.invalid/'||g,'https://plan.invalid/'||g,g::text,'plan.invalid','ready','ready','new' FROM generate_series(1000001,1095000) g`,
		fmt.Sprintf(`INSERT INTO article_dedupe_members(run_id,representation_id,representative_id,material_id,version_id,candidate_id,similarity,method,applied)
 SELECT %d,g,CASE WHEN g%%400=0 THEN g-1 ELSE g END,g,g,g,1,'unique',TRUE FROM generate_series(1000001,1095000) g`, runID),
	}
	if err := database.Transaction(func(tx *gorm.DB) error {
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"material", "material_versions", "material_representations", "material_article_states", "discovery_candidates", "article_dedupe_members", "discovery_domain_blacklist_entries"} {
		if err := database.Exec("ANALYZE " + table).Error; err != nil {
			t.Fatal(err)
		}
	}
	query, err := pendingAssessmentTargetsQuery(database, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct{ MaterialID, ContentVersion uint }
	statement := query.Session(&gorm.Session{DryRun: true}).Find(&rows).Statement
	var encoded string
	if err := database.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement.SQL.String(), statement.Vars...).Scan(&encoded).Error; err != nil {
		t.Fatal(err)
	}
	type planNode struct {
		Rows    int64      `json:"Plan Rows"`
		Actual  int64      `json:"Actual Rows"`
		Node    string     `json:"Node Type"`
		Plans   []planNode `json:"Plans"`
		Buffers int64      `json:"Shared Hit Blocks"`
	}
	var plan []struct {
		Plan planNode
		Time float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal([]byte(encoded), &plan); err != nil || len(plan) != 1 {
		t.Fatal("invalid EXPLAIN JSON", err)
	}
	top := plan[0].Plan
	if top.Actual < size*9/10 || top.Rows < top.Actual/10 || top.Rows > top.Actual*10 {
		t.Fatalf("representative row estimate is still pathological: estimate=%d actual=%d", top.Rows, top.Actual)
	}
	targets, err := PendingAssessmentTargets(context.Background(), database, false, nil)
	if err != nil || len(targets) != int(top.Actual) {
		t.Fatalf("target results differ from analyzed plan: targets=%d rows=%d err=%v", len(targets), top.Actual, err)
	}
	t.Logf("95k members: plan_rows=%d actual_rows=%d execution_ms=%.3f shared_hits=%d", top.Rows, top.Actual, plan[0].Time, top.Buffers)
	// Recreate the investigated SQL for a same-database comparison. Both
	// variants must return the same set size; elapsed time is diagnostic only.
	baseline := Candidates(database).Select("id, material_id, content_version").
		Where("processing_state='ready' AND assessment_state='pending'").
		Where(DedupeRepresentativeClause("discovery_candidates.")).
		Where(`EXISTS (SELECT 1 FROM article_dedupe_members d JOIN material m ON m.id=d.material_id AND m.current_version_id=d.version_id
 WHERE d.run_id=? AND d.material_id=discovery_candidates.material_id AND d.representation_id=d.representative_id AND d.applied)`, runID)
	baseline = ExcludeBlacklistedCandidateDomains(baseline, "").Order("id")
	var originalRows []DiscoveryCandidate
	old := baseline.Session(&gorm.Session{DryRun: true}).Find(&originalRows).Statement
	oldSQL := strings.ReplaceAll(old.SQL.String(), "LIMIT 1 OFFSET 0", "")
	for attempt := 1; attempt <= 3; attempt++ {
		for _, variant := range []struct {
			name string
			sql  string
			args []interface{}
		}{{"before", oldSQL, old.Vars}, {"after", statement.SQL.String(), statement.Vars}} {
			if err := database.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+variant.sql, variant.args...).Scan(&encoded).Error; err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(encoded), &plan); err != nil {
				t.Fatal(err)
			}
			if plan[0].Plan.Actual != top.Actual {
				t.Fatal("optimized query changed the representative result count")
			}
			t.Logf("comparison %d %s: plan_rows=%d actual_rows=%d execution_ms=%.3f shared_hits=%d", attempt, variant.name, plan[0].Plan.Rows, plan[0].Plan.Actual, plan[0].Time, plan[0].Plan.Buffers)
		}
	}
	var repairRows []struct{ RepresentationID uint }
	gap := dedupeRecoveryCandidates(database, runID).Session(&gorm.Session{DryRun: true}).Find(&repairRows).Statement
	if err := database.Raw("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+gap.SQL.String(), gap.Vars...).Scan(&encoded).Error; err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(encoded), &plan); err != nil {
		t.Fatal(err)
	}
	if plan[0].Plan.Actual != 0 {
		t.Fatalf("converged library still has repair targets: %d", plan[0].Plan.Actual)
	}
	t.Logf("95k members coverage sweep: execution_ms=%.3f shared_hits=%d", plan[0].Time, plan[0].Plan.Buffers)
}
