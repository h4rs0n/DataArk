package discovery

import (
	"DataArk/dedupe"
	"DataArk/jobqueue"
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func activateRecoveryFixture(t *testing.T, candidates ...DiscoveryCandidate) dedupe.Run {
	t.Helper()
	if _, err := lockDedupeSetting(db); err != nil {
		t.Fatal(err)
	}
	run := dedupe.Run{Algorithm: dedupe.Algorithm, Status: "active"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		var article dedupeArticle
		if err := dedupeArticles(db).Where("m.id=?", candidate.MaterialID).Scan(&article).Error; err != nil {
			t.Fatal(err)
		}
		member := dedupe.Member{RunID: run.ID, RepresentationID: article.RepresentationID, RepresentativeID: article.RepresentationID, MaterialID: article.MaterialID, VersionID: article.VersionID, CandidateID: article.CandidateID, Applied: true, Method: "unique", Revision: 1}
		if err := db.Create(&member).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&dedupe.Setting{}).Where("id=1").Updates(map[string]interface{}{"mode": "active", "active_run_id": run.ID}).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestCrawlRestrictedContentRecoversDedupeAndConverges(t *testing.T) {
	setupSQLiteDB(t)
	startDedupeTestQueue(t)
	candidate := newIndexedCandidate(t, "blocked-idle", dedupeAutoBody())
	run := activateRecoveryFixture(t, candidate)
	rule, err := CreateDiscoveryDomainBlacklist("blocked-idle.example", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&dedupe.Member{}).Where("run_id=?", run.ID).Update("applied", false).Error; err != nil {
		t.Fatal(err)
	}
	if drift, err := dedupeRecoveryDrift(context.Background(), run.ID); err != nil || drift == 0 {
		t.Fatalf("retained content not actionable: %d %v", drift, err)
	}
	if err := RecoverDedupeJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitDedupeIdle(t)
	for i := 0; i < 3; i++ {
		if drift, err := dedupeRecoveryDrift(context.Background(), run.ID); err != nil || drift != 0 {
			t.Fatalf("drift=%d err=%v", drift, err)
		}
		if work, err := recoverDedupeJobs(context.Background(), i == 2); err != nil || work != 0 {
			t.Fatalf("recovery work=%d err=%v", work, err)
		}
		waitDedupeIdle(t)
	}
	qualification, err := ArticleDedupeQualification(db, candidate.MaterialID)
	if err != nil || !qualification.Ready || !qualification.Representative {
		t.Fatalf("qualification=%+v err=%v", qualification, err)
	}
	if _, err := DeleteDiscoveryDomainBlacklist(rule.Entry.ID); err != nil {
		t.Fatal(err)
	}
	if drift, err := dedupeRecoveryDrift(context.Background(), run.ID); err != nil || drift != 0 {
		t.Fatalf("deletion created drift: %d %v", drift, err)
	}
}

func TestBatchPruningMatchesArticleQualification(t *testing.T) {
	setupSQLiteDB(t)
	first := newIndexedCandidate(t, "batch-representative", dedupeAutoBody())
	second := newIndexedCandidate(t, "batch-covered", dedupeAutoBody()+" note")
	run := activateRecoveryFixture(t, first, second)
	var representative dedupe.Member
	if err := db.First(&representative, "run_id=? AND material_id=?", run.ID, first.MaterialID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&dedupe.Member{}).Where("run_id=? AND material_id=?", run.ID, second.MaterialID).Update("representative_id", representative.RepresentationID).Error; err != nil {
		t.Fatal(err)
	}
	targets := []jobqueue.AssessmentTarget{{MaterialID: first.MaterialID, ContentVersion: "1"}, {MaterialID: second.MaterialID, ContentVersion: "1"}, {MaterialID: second.MaterialID, ContentVersion: "0"}, {MaterialID: 99999}}
	check := func(t *testing.T) {
		t.Helper()
		actual, err := suppressedDedupeAssessmentTargets(db, run.ID, targets)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range targets {
			q, err := ArticleDedupeQualification(db, target.MaterialID)
			if err != nil {
				t.Fatal(err)
			}
			if actual[target.MaterialID] != (q.Active && q.Ready && !q.Representative) {
				t.Fatalf("material %d batch=%t qualification=%+v", target.MaterialID, actual[target.MaterialID], q)
			}
		}
	}
	check(t)
	mutation, err := CreateDiscoveryDomainBlacklist("batch-representative.example", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	check(t)
	if _, err := DeleteDiscoveryDomainBlacklist(mutation.Entry.ID); err != nil {
		t.Fatal(err)
	}
	check(t)
	if err := db.Model(&DiscoveryCandidate{}).Where("id=?", first.ID).Update("processing_state", DiscoveryProcessingFailed).Error; err != nil {
		t.Fatal(err)
	}
	check(t)
	if err := db.Model(&DiscoveryCandidate{}).Where("id=?", first.ID).Update("processing_state", DiscoveryProcessingReady).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&dedupe.Member{}).Where("run_id=? AND material_id=?", run.ID, first.MaterialID).Update("candidate_id", 0).Error; err != nil {
		t.Fatal(err)
	}
	check(t) // changed authoritative ready candidate invalidates coverage
	if err := db.Model(&dedupe.Member{}).Where("run_id=? AND material_id=?", run.ID, first.MaterialID).Update("candidate_id", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := UpdateCandidate(db, first.ID, map[string]interface{}{"body_text": dedupeAutoBody() + " changed version"}).Error; err != nil {
		t.Fatal(err)
	}
	check(t) // obsolete representative must not suppress queued assessments
	if err := db.Model(&dedupe.Setting{}).Where("id=1").Update("mode", "exact").Error; err != nil {
		t.Fatal(err)
	}
	check(t)
}

func TestPendingAssessmentTargetsMatchExistingGates(t *testing.T) {
	setupSQLiteDB(t)
	first := newIndexedCandidate(t, "targets-first", dedupeAutoBody())
	second := newIndexedCandidate(t, "targets-second", "different body")
	blocked := newIndexedCandidate(t, "targets-blocked", "blocked body")
	run := activateRecoveryFixture(t, first, second, blocked)
	if _, err := CreateDiscoveryDomainBlacklist("targets-blocked.example", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCandidate(db, second.ID, map[string]interface{}{"body_text": "new current version"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"active", "exact", "shadow"} {
		t.Run(mode, func(t *testing.T) {
			if err := db.Model(&dedupe.Setting{}).Where("id=1").Updates(map[string]interface{}{"mode": mode, "active_run_id": run.ID}).Error; err != nil {
				t.Fatal(err)
			}
			for _, ready := range []bool{false, true} {
				now := time.Now()
				actual, err := PendingAssessmentTargets(context.Background(), db, ready, &now)
				if err != nil {
					t.Fatal(err)
				}
				query := Candidates(db).Select("id, material_id, content_version").Where("processing_state='ready' AND assessment_state='pending'").Where("next_processing_at IS NULL OR next_processing_at<=?", now)
				if ready {
					query = query.Where("dedupe_state='ready'")
				}
				query, err = FilterDedupeAssessmentCandidates(query)
				if err != nil {
					t.Fatal(err)
				}
				var rows []DiscoveryCandidate
				if err := query.Order("id").Find(&rows).Error; err != nil {
					t.Fatal(err)
				}
				want := []jobqueue.AssessmentTarget{}
				for _, row := range rows {
					want = append(want, jobqueue.AssessmentTarget{MaterialID: row.MaterialID, ContentVersion: fmt.Sprint(row.ContentVersion)})
				}
				if !reflect.DeepEqual(actual, want) {
					t.Fatalf("ready=%t actual=%+v want=%+v", ready, actual, want)
				}
			}
		})
	}
}
