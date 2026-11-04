package discovery

import (
	"DataArk/dedupe"
	"DataArk/jobqueue"
	"DataArk/material"
	"context"
	"fmt"
	"gorm.io/gorm/logger"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func startDedupeTestQueue(t *testing.T) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	db.Logger = logger.Default.LogMode(logger.Silent)
	stop, err := jobqueue.Start(context.Background(), db, jobqueue.Handlers{AssessArticle: func(context.Context, uint, string) error { return nil }, Dedupe: func(ctx context.Context, args jobqueue.DedupeArgs) error {
		activeDedupeTestWorkers.Add(1)
		defer activeDedupeTestWorkers.Add(-1)
		return RunDedupeTask(ctx, args)
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { waitDedupeIdle(t); stop() })
}
func waitDedupeStatus(t *testing.T, id uint, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var run dedupe.Run
		if err := db.First(&run, id).Error; err != nil {
			t.Fatal(err)
		}
		if run.Status == want {
			waitDedupeIdle(t)
			return
		}
		if run.Error != "" {
			t.Fatalf("run failed: %s", run.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %d did not reach %s", id, want)
}
func newIndexedCandidate(t *testing.T, name, body string) DiscoveryCandidate {
	t.Helper()
	now := time.Now()
	return createDedupeCandidate(t, "https://"+name+".example/post", "https://"+name+".example/post", "https://"+name+".example/post", body, now)
}
func TestDedupePreviewApplyCoverageAndRollback(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: A measured system explains queue contention, deployment failures, and recovery mechanisms. ", i)
	}
	first := newIndexedCandidate(t, "first", body)
	second := newIndexedCandidate(t, "second", strings.Replace(body, "measured", "tested", 1))
	third := newIndexedCandidate(t, "third", strings.Repeat("A gardening article discusses soil and seasonal flowers with practical observations. ", 12))
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	var unchanged DiscoveryCandidate
	if err := db.First(&unchanged, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.RepresentativeID != nil {
		t.Fatal("preview modified production projection")
	}
	report, err := GetDedupeReport(context.Background(), run.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if report.Representatives != 2 || report.NearCovered != 1 || report.IndexedCurrent != 3 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if _, err := RequestDedupeAction(context.Background(), run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	q, err := ArticleDedupeQualification(db, second.MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Active || !q.Ready || q.Representative || q.Coverage != "waiting_representative" {
		t.Fatalf("unexpected member qualification %+v", q)
	}
	var member DiscoveryCandidate
	if err := db.First(&member, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if member.CurrentAssessmentID != nil || member.AssessmentState == "ready" || member.EligibilityReasons != "duplicate_non_representative" {
		t.Fatalf("member was falsely assessed %+v", member)
	}
	detail, err := GetDedupeCluster(context.Background(), member.DuplicateClusterID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Members) != 2 {
		t.Fatalf("cluster members %+v", detail)
	}
	for _, candidate := range []DiscoveryCandidate{first, third} {
		q, err := ArticleDedupeQualification(db, candidate.MaterialID)
		if err != nil {
			t.Fatal(err)
		}
		if !q.Representative {
			t.Fatalf("independent article suppressed %+v", q)
		}
	}
	if _, err := RequestDedupeAction(context.Background(), run.ID, "rollback"); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "rolled_back")
	if err := db.First(&member, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if member.RepresentativeID != nil || member.EligibilityReasons == "duplicate_non_representative" {
		t.Fatal("rollback did not restore projection")
	}
	q, err = ArticleDedupeQualification(db, second.MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if !q.ExactOnly || !q.Representative {
		t.Fatalf("rollback qualification %+v", q)
	}
}
func TestDedupeUpdatedBodyInvalidatesCoverage(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("章节 %d：实验测量延迟和内存占用，并描述算法实现及验证过程中遇到的问题。", i)
	}
	first := newIndexedCandidate(t, "version-a", body)
	second := newIndexedCandidate(t, "version-b", body+"补充说明。")
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	if _, err := RequestDedupeAction(context.Background(), run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	if err := UpdateCandidate(db, first.ID, map[string]interface{}{"body_text": strings.Repeat("A completely new independent essay about gardening and seasonal soil preparation. ", 12), "content_version": uint(2)}).Error; err != nil {
		t.Fatal(err)
	}
	q, err := ArticleDedupeQualification(db, second.MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if q.Ready {
		t.Fatal("old representative version remained usable")
	}
	var article dedupeArticle
	if err := dedupeArticles(db).Where("m.id=?", second.MaterialID).Scan(&article).Error; err != nil {
		t.Fatal(err)
	}
	if err := processIncrementalDedupe(context.Background(), article.RepresentationID); err != nil {
		t.Fatal(err)
	}
	q, err = ArticleDedupeQualification(db, second.MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Ready || !q.Representative {
		t.Fatalf("member not promoted %+v", q)
	}
}
func TestDedupeFindsCandidateOutsideLegacyWindow(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	// A matching article is deliberately after the old first-500 cutoff.
	for i := 0; i < 501; i++ {
		newIndexedCandidate(t, fmt.Sprintf("unrelated-%d", i), fmt.Sprintf("Independent record %d. ", i)+strings.Repeat("different filler observations about operational engineering. ", 5))
	}
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: A carefully measured argument explains uncommon interactions across cache and queue boundaries. ", i)
	}
	first := newIndexedCandidate(t, "late-first", body)
	second := newIndexedCandidate(t, "late-second", body+" Note.")
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	var articles []dedupeArticle
	if err := dedupeArticles(db).Where("m.id IN ?", []uint{first.MaterialID, second.MaterialID}).Scan(&articles).Error; err != nil {
		t.Fatal(err)
	}
	var members []dedupe.Member
	if err := db.Where("run_id=? AND material_id IN ?", run.ID, []uint{first.MaterialID, second.MaterialID}).Find(&members).Error; err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].RepresentativeID != members[1].RepresentativeID {
		t.Fatalf("global retrieval missed duplicate %+v", members)
	}
	// Every accepted member has a direct relation to its representative.
	for _, member := range members {
		var rep material.Representation
		if err := db.First(&rep, member.RepresentativeID).Error; err != nil {
			t.Fatal(err)
		}
		article, err := loadDedupeArticle(db, member.RepresentationID)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := dedupe.Verify(article.Text, rep.Text); !ok {
			t.Fatal("invalid cluster")
		}
	}
}

var activeDedupeTestWorkers atomic.Int64

func waitDedupeIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if activeDedupeTestWorkers.Load() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("dedupe worker did not become idle")
}

func TestDedupePreviewSnapshotAndStaleApplication(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: Measurements explain queue contention and controlled recovery experiments. ", i)
	}
	first := newIndexedCandidate(t, "snapshot-a", body)
	second := newIndexedCandidate(t, "snapshot-b", body+" Note.")
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	repeated, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if repeated.ID != run.ID {
		t.Fatal("duplicate preview request created another run")
	}
	third := newIndexedCandidate(t, "snapshot-c", body+" New note.")
	var newArticle dedupeArticle
	if err := dedupeArticles(db).Where("m.id=?", third.MaterialID).Scan(&newArticle).Error; err != nil {
		t.Fatal(err)
	}
	if newArticle.RepresentationID <= run.HighWaterID {
		t.Fatal("new article was not outside snapshot")
	}
	if err := UpdateCandidate(db, first.ID, map[string]interface{}{"body_text": strings.Repeat("Independent gardening observations about soil, roots, and seasonal plants. ", 10), "content_version": uint(2)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := RequestDedupeAction(context.Background(), run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	q, err := ArticleDedupeQualification(db, second.MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Ready || !q.Representative {
		t.Fatalf("obsolete preview suppressed surviving article %+v", q)
	}
	// Application recovery also indexed articles which arrived after the snapshot.
	q, err = ArticleDedupeQualification(db, third.MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Ready || q.Representative {
		t.Fatalf("new article did not join surviving representative %+v", q)
	}
	if _, err := RequestDedupeAction(context.Background(), run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
}
func TestDedupeRepresentativeIgnoresCrawlRestrictionChanges(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	first := newIndexedCandidate(t, "restricted-original", dedupeAutoBody())
	second := newIndexedCandidate(t, "surviving-repost", dedupeAutoBody()+" Note.")
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	if _, err := RequestDedupeAction(context.Background(), run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	check := func() {
		t.Helper()
		for _, c := range []DiscoveryCandidate{first, second} {
			q, err := ArticleDedupeQualification(db, c.MaterialID)
			if err != nil || !q.Ready || q.Representative != (c.ID == first.ID) {
				t.Fatalf("qualification changed: %+v %v", q, err)
			}
			var current DiscoveryCandidate
			if err := db.First(&current, c.ID).Error; err != nil || current.DedupeState != DiscoveryDedupeReady {
				t.Fatalf("dedupe state changed: %+v %v", current, err)
			}
		}
	}
	check()
	rule, err := CreateDiscoveryDomainBlacklist("restricted-original.example", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	check()
	if err := RecoverDedupeJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitDedupeIdle(t)
	check()
	if _, err := DeleteDiscoveryDomainBlacklist(rule.Entry.ID); err != nil {
		t.Fatal(err)
	}
	check()
}
func TestDedupeNeverCreatesTransitiveCoverage(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	// Distinct shingles make two edits overlap the intermediate text but not the endpoints.
	var generated strings.Builder
	seed := uint64(7)
	for i := 0; i < 2200; i++ {
		seed = seed*6364136223846793005 + 1
		generated.WriteByte(byte('a' + (seed>>32)%26))
	}
	a := generated.String()
	b := []byte(a)
	for i := 0; i < 7; i++ {
		copy(b[100+i*250:105+i*250], "ZZZZZ")
	}
	c := append([]byte{}, b...)
	for i := 0; i < 7; i++ {
		copy(c[180+i*250:185+i*250], "YYYYY")
	}
	if _, ok := dedupe.Verify(a, string(b)); !ok {
		t.Fatal("A/B fixture not near duplicate")
	}
	if _, ok := dedupe.Verify(string(b), string(c)); !ok {
		t.Fatal("B/C fixture not near duplicate")
	}
	if _, ok := dedupe.Verify(a, string(c)); ok {
		t.Fatal("A/C fixture unexpectedly matches")
	}
	newIndexedCandidate(t, "chain-a", a)
	newIndexedCandidate(t, "chain-b", string(b))
	newIndexedCandidate(t, "chain-c", string(c))
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	var members []dedupe.Member
	if err := db.Where("run_id=?", run.ID).Find(&members).Error; err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		article, err := loadDedupeArticle(db, member.RepresentationID)
		if err != nil {
			t.Fatal(err)
		}
		representative, err := loadDedupeArticle(db, member.RepresentativeID)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := dedupe.Verify(article.Text, representative.Text); !ok {
			t.Fatalf("transitive coverage %+v", member)
		}
	}
	if members[0].RepresentativeID == members[2].RepresentativeID {
		t.Fatal("stable representative allowed chain expansion")
	}
}

func TestDedupeCompletenessBeforeAssessmentReuse(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	if err := db.Exec(`CREATE TABLE discovery_article_assessments (id integer PRIMARY KEY, material_id integer, content_version integer, assessor text, assessor_version text, policy_version text)`).Error; err != nil {
		t.Fatal(err)
	}
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: A measured system explains queue contention and recovery mechanisms. ", i)
	}
	first := newIndexedCandidate(t, "incomplete-assessed", body)
	complete := body + strings.Repeat("queue contention and recovery mechanisms. ", 6)
	second := newIndexedCandidate(t, "complete-unassessed", complete)
	if _, ok := dedupe.Verify(body, complete); !ok {
		t.Fatal("completeness fixture does not match")
	}
	if err := db.Exec("INSERT INTO discovery_article_assessments VALUES(999,?,1,?,?,?)", first.MaterialID, "openai_compatible", "configured", "article-value-v4").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&material.ArticleState{}).Where("material_id=?", first.MaterialID).Updates(map[string]interface{}{"current_assessment_id": uint(999), "assessment_state": "ready", "quality_score": 0.9, "eligibility_state": DiscoveryEligibilityEligible}).Error; err != nil {
		t.Fatal(err)
	}
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	var members []dedupe.Member
	if err := db.Where("run_id=?", run.ID).Order("representation_id").Find(&members).Error; err != nil {
		t.Fatal(err)
	}
	var article dedupeArticle
	if err := dedupeArticles(db).Where("m.id=?", second.MaterialID).Scan(&article).Error; err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 || members[0].RepresentativeID != article.RepresentationID || members[1].RepresentativeID != article.RepresentationID {
		t.Fatalf("incomplete assessment overrode completeness %+v", members)
	}
	if _, err := RequestDedupeAction(context.Background(), run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	var state material.ArticleState
	if err := db.First(&state, "material_id=?", first.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	if state.CurrentAssessmentID == nil || *state.CurrentAssessmentID != 999 {
		t.Fatal("independent assessment was replaced")
	}
}

func dedupeIDSet(ids []uint) map[uint]bool {
	set := make(map[uint]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// TestDedupeAssessmentGateStaysInsideRecommendationGate pins the invariant that
// a candidate the pipeline will pay to assess is one recommendation can surface.
// A wider assessment gate spends LLM calls on permanently unreachable articles.
func TestDedupeAssessmentGateStaysInsideRecommendationGate(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: A measured system explains queue contention and recovery mechanisms. ", i)
	}
	first := newIndexedCandidate(t, "gate-original", body)
	second := newIndexedCandidate(t, "gate-repost", strings.Replace(body, "measured", "tested", 1))
	third := newIndexedCandidate(t, "gate-unrelated", strings.Repeat("A gardening article discusses soil and seasonal flowers with practical observations. ", 12))
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	if _, err := RequestDedupeAction(context.Background(), run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	var covered DiscoveryCandidate
	if err := db.First(&covered, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if covered.RepresentativeID == nil || *covered.RepresentativeID != first.ID {
		t.Fatalf("fixture did not produce a covered member: %+v", covered)
	}
	for _, mode := range []string{"shadow", "active", "exact"} {
		if err := db.Model(&dedupe.Setting{}).Where("id=?", 1).Update("mode", mode).Error; err != nil {
			t.Fatal(err)
		}
		query, err := FilterDedupeAssessmentCandidates(Candidates(db))
		if err != nil {
			t.Fatal(err)
		}
		var assessable []uint
		if err := query.Pluck("discovery_candidates.id", &assessable).Error; err != nil {
			t.Fatal(err)
		}
		var recommendable []uint
		if err := Candidates(db).Where(DedupeRepresentativeClause("discovery_candidates.")).Pluck("discovery_candidates.id", &recommendable).Error; err != nil {
			t.Fatal(err)
		}
		surfaced := dedupeIDSet(recommendable)
		for _, id := range assessable {
			if !surfaced[id] {
				t.Fatalf("mode %s: candidate %d is assessable but never recommendable", mode, id)
			}
		}
		assessed := dedupeIDSet(assessable)
		if assessed[second.ID] {
			t.Fatalf("mode %s: covered member %d is still assessable", mode, second.ID)
		}
		for _, want := range []uint{first.ID, third.ID} {
			if !assessed[want] {
				t.Fatalf("mode %s: independent candidate %d was suppressed", mode, want)
			}
		}
	}
}

// TestDedupeReportCountsReusedAssessments guards the single-aggregate rewrite of
// existingAssessmentReused, which the SQLite suite cannot otherwise observe.
func TestDedupeReportCountsReusedAssessments(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	if err := db.Exec(`CREATE TABLE discovery_article_assessments (id integer PRIMARY KEY, material_id integer, content_version integer, assessor text, assessor_version text, policy_version text)`).Error; err != nil {
		t.Fatal(err)
	}
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: A measured system explains queue contention and recovery mechanisms. ", i)
	}
	first := newIndexedCandidate(t, "reused-assessed", body)
	newIndexedCandidate(t, "reused-repost", strings.Replace(body, "measured", "tested", 1))
	if err := db.Exec("INSERT INTO discovery_article_assessments VALUES(999,?,1,?,?,?)", first.MaterialID, "openai_compatible", "configured", "article-value-v4").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&material.ArticleState{}).Where("material_id=?", first.MaterialID).Updates(map[string]interface{}{"current_assessment_id": uint(999), "assessment_state": "ready", "quality_score": 0.9, "eligibility_state": DiscoveryEligibilityEligible}).Error; err != nil {
		t.Fatal(err)
	}
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	report, err := GetDedupeReport(context.Background(), run.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if report.Representatives != 1 || report.NearCovered != 1 {
		t.Fatalf("fixture did not cluster as expected: %+v", report)
	}
	if report.ExistingAssessmentReused != 1 {
		t.Fatalf("existingAssessmentReused = %d, want 1", report.ExistingAssessmentReused)
	}
}

// TestDedupeRecoveryDriftProbe verifies the cheap gate that keeps the periodic
// recovery sweep off a converged library.
func TestDedupeRecoveryDriftProbe(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: A measured system explains queue contention and recovery mechanisms. ", i)
	}
	newIndexedCandidate(t, "drift-original", body)
	third := newIndexedCandidate(t, "drift-unrelated", strings.Repeat("A gardening article discusses soil and seasonal flowers with practical observations. ", 12))
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	if _, err := RequestDedupeAction(context.Background(), run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	drift, err := dedupeRecoveryDrift(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if drift != 0 {
		t.Fatalf("converged library reported drift %d", drift)
	}
	if err := db.Model(&DiscoveryCandidate{}).Where("id=?", third.ID).Update("dedupe_state", DiscoveryDedupePending).Error; err != nil {
		t.Fatal(err)
	}
	drift, err = dedupeRecoveryDrift(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if drift == 0 {
		t.Fatal("missed a candidate whose extraction finished without dedupe")
	}
}

func setDedupeTestReleaseHook(t *testing.T) *atomic.Int32 {
	t.Helper()
	old := ReleaseDeferredAssessment
	var releases atomic.Int32
	ReleaseDeferredAssessment = func(context.Context) error {
		releases.Add(1)
		return nil
	}
	t.Cleanup(func() { ReleaseDeferredAssessment = old })
	return &releases
}

func dedupeAutoBody() string {
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: A measured system explains queue contention and recovery mechanisms. ", i)
	}
	return body
}

// TestEnsureDedupeForAssessmentBuildsAppliesAndReleases proves one assessment
// entry point drives the whole dedupe pipeline without operator calls: shadow
// mode defers the run, auto-apply carries indexing through applying to active,
// and the deferred assessment is released exactly once suppression is live.
func TestEnsureDedupeForAssessmentBuildsAppliesAndReleases(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	releases := setDedupeTestReleaseHook(t)
	body := dedupeAutoBody()
	first := newIndexedCandidate(t, "auto-first", body)
	second := newIndexedCandidate(t, "auto-second", body+" Note.")
	run, proceed, err := EnsureDedupeForAssessment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if proceed {
		t.Fatal("shadow mode must defer assessment until suppression is live")
	}
	if !run.AutoApply {
		t.Fatalf("run %+v must carry the auto-apply intent", run)
	}
	var setting dedupe.Setting
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if !setting.PendingAssessment {
		t.Fatal("deferred assessment intent was not recorded")
	}
	pendingRun, pending, err := PendingDedupeAssessmentRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !pending || pendingRun.ID != run.ID {
		t.Fatalf("pending run = %+v (pending=%v), want run %d", pendingRun, pending, run.ID)
	}
	waitDedupeStatus(t, run.ID, "active")
	if releases.Load() == 0 {
		t.Fatal("deferred assessment was not released after activation")
	}
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if setting.Mode != "active" || setting.ActiveRunID != run.ID {
		t.Fatalf("unexpected setting %+v", setting)
	}
	if setting.PendingAssessment {
		t.Fatal("release must clear the durable intent")
	}
	if _, pending, err := PendingDedupeAssessmentRun(context.Background()); err != nil {
		t.Fatal(err)
	} else if pending {
		t.Fatal("released assessment must stop reporting a pending dedupe run")
	}
	qual, err := ArticleDedupeQualification(db, second.MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if !qual.Active || !qual.Ready || qual.Representative {
		t.Fatalf("duplicate survived suppression: %+v", qual)
	}
	qual, err = ArticleDedupeQualification(db, first.MaterialID)
	if err != nil {
		t.Fatal(err)
	}
	if !qual.Active || !qual.Ready || !qual.Representative {
		t.Fatalf("representative was suppressed: %+v", qual)
	}
	activeRun, proceed, err := EnsureDedupeForAssessment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !proceed || activeRun.ID != 0 {
		t.Fatalf("active mode must proceed without building: run=%+v proceed=%v", activeRun, proceed)
	}
	var runs int64
	if err := db.Model(&dedupe.Run{}).Count(&runs).Error; err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("active mode created extra runs: %d", runs)
	}
}

// TestEnsureDedupeForAssessmentRespectsExactMode pins the safety property that
// an operator rollback is never silently undone: exact mode proceeds to
// assessment under exact-only gating without building a run or deferring.
func TestEnsureDedupeForAssessmentRespectsExactMode(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	releases := setDedupeTestReleaseHook(t)
	if err := db.Create(&dedupe.Setting{ID: 1, Mode: "exact"}).Error; err != nil {
		t.Fatal(err)
	}
	_, proceed, err := EnsureDedupeForAssessment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !proceed {
		t.Fatal("exact mode must proceed immediately")
	}
	var runs int64
	if err := db.Model(&dedupe.Run{}).Count(&runs).Error; err != nil {
		t.Fatal(err)
	}
	if runs != 0 {
		t.Fatalf("exact mode must not build runs, got %d", runs)
	}
	var setting dedupe.Setting
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if setting.PendingAssessment {
		t.Fatal("exact mode must not record a deferred intent")
	}
	if releases.Load() != 0 {
		t.Fatal("exact mode must not release anything")
	}
}

// TestDedupeRecoveryReleasesPendingAssessment covers the crash window between
// activation and release: the durable intent survives and recovery releases it.
func TestDedupeRecoveryReleasesPendingAssessment(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	releases := setDedupeTestReleaseHook(t)
	body := dedupeAutoBody()
	newIndexedCandidate(t, "rec-first", body)
	newIndexedCandidate(t, "rec-second", body+" Note.")
	run, _, err := EnsureDedupeForAssessment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	if releases.Load() == 0 {
		t.Fatal("initial release did not happen")
	}
	if err := db.Model(&dedupe.Setting{}).Where("id=?", 1).Update("pending_assessment", true).Error; err != nil {
		t.Fatal(err)
	}
	before := releases.Load()
	if err := RecoverDedupeJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if releases.Load() <= before {
		t.Fatal("recovery did not release the pending assessment")
	}
	var setting dedupe.Setting
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if setting.PendingAssessment {
		t.Fatal("recovery must clear the durable intent after release")
	}
}

// TestDedupeRecoveryPushesStalledAutoApplyRun covers the crash between marking
// a reused ready run for auto-apply and requesting the apply transition.
func TestDedupeRecoveryPushesStalledAutoApplyRun(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	releases := setDedupeTestReleaseHook(t)
	body := dedupeAutoBody()
	newIndexedCandidate(t, "stall-first", body)
	newIndexedCandidate(t, "stall-second", body+" Note.")
	run, err := CreateDedupeRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "ready")
	if err := db.Model(&dedupe.Run{}).Where("id=?", run.ID).Update("auto_apply", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&dedupe.Setting{}).Where("id=?", 1).Update("pending_assessment", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := RecoverDedupeJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitDedupeStatus(t, run.ID, "active")
	if releases.Load() == 0 {
		t.Fatal("stalled auto-apply run never released the assessment")
	}
	var setting dedupe.Setting
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if setting.PendingAssessment {
		t.Fatal("recovery must clear the durable intent after release")
	}
}
