package bootstrap

import (
	"DataArk/assessment"
	"DataArk/config"
	"DataArk/dedupe"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/material"
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

type postgresDedupeProvider struct{ calls atomic.Int32 }

func (p *postgresDedupeProvider) AssessArticle(context.Context, assessment.ChatAssessmentInput) (assessment.ChatAssessmentResult, error) {
	p.calls.Add(1)
	return assessment.ChatAssessmentResult{QualityScore: 92, DepthScore: 80, EvergreenScore: 90, Reasons: []string{"包含具体测量和机制解释", "实验范围有限"}, Summary: "通过实测解释队列和缓存的权衡。", Keywords: []string{"队列", "缓存", "测量"}}, nil
}
func verifyPostgresDedupe(t *testing.T, database *gorm.DB) {
	ctx := context.Background()
	oldModel := config.LLMCHATMODEL
	config.LLMCHATMODEL = "dedupe-fixture"
	defer func() { config.LLMCHATMODEL = oldModel }()
	provider := &postgresDedupeProvider{}
	assessor := assessment.EnrichmentArticleAssessor{Provider: provider, Model: config.LLMCHATMODEL}
	source := discovery.DiscoverySource{Name: "Dedupe fixture", URL: "https://dedupe-fixture.invalid/feed", Type: discovery.DiscoverySourceTypeFeed, EndpointType: discovery.DiscoveryEndpointFeed, Enabled: true}
	if err := database.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	body := ""
	for i := 0; i < 40; i++ {
		body += fmt.Sprintf("Section %d: A measured system explains queue contention, deployment failures, and recovery mechanisms. ", i)
	}
	create := func(name, text string) discovery.DiscoveryCandidate {
		candidate := discovery.DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: "https://dedupe-fixture.invalid/" + name, NormalizedURL: "https://dedupe-fixture.invalid/" + name, Title: "Queue measurements", BodyText: text, ContentHash: material.TextHash(text), ContentVersion: 1, Language: "en", WordCount: len(strings.Fields(text)), ProcessingState: discovery.DiscoveryProcessingReady, DedupeState: discovery.DiscoveryDedupeReady, AssessmentState: discovery.DiscoveryAssessmentPending, EligibilityState: discovery.DiscoveryEligibilityUnknown, LastSeenAt: time.Now()}
		if err := database.Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
		return candidate
	}
	first := create("original", body)
	second := create("repost", strings.Replace(body, "measured", "tested", 1))
	if err := assessment.AssessMaterial(ctx, first.MaterialID, "1", assessor); err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 1 {
		t.Fatal("initial assessment not executed")
	}
	stop, err := jobqueue.Start(ctx, database, jobqueue.Handlers{Dedupe: discovery.RunDedupeTask, AssessArticle: func(ctx context.Context, id uint, version string) error {
		return assessment.AssessMaterial(ctx, id, version, assessor)
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	queue, _ := jobqueue.Default()
	if err := queue.EnqueueAssessArticle(ctx, second.MaterialID, "1"); err != nil {
		t.Fatal(err)
	}
	run, err := discovery.CreateDedupeRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitForPostgresCondition(t, 20*time.Second, func() bool {
		var current dedupe.Run
		return database.First(&current, run.ID).Error == nil && current.Status == "ready"
	})
	report, err := discovery.GetDedupeReport(ctx, run.ID, 0, 250)
	if err != nil {
		t.Fatal(err)
	}
	if report.NearCovered < 1 || report.ExistingAssessmentReused < 1 {
		t.Fatalf("dedupe report %+v", report)
	}
	if _, err := discovery.RequestDedupeAction(ctx, run.ID, "apply"); err != nil {
		t.Fatal(err)
	}
	waitForPostgresCondition(t, 20*time.Second, func() bool {
		q, err := discovery.ArticleDedupeQualification(database, second.MaterialID)
		return err == nil && q.Coverage == "covered"
	})
	waitForPostgresCondition(t, 20*time.Second, func() bool {
		var current dedupe.Run
		return database.First(&current, run.ID).Error == nil && current.Cancelled >= 1
	})
	if err := assessment.AssessMaterial(ctx, second.MaterialID, "1", assessor); err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 1 {
		t.Fatal("covered member performed LLM call")
	}
	var member discovery.DiscoveryCandidate
	member = discovery.DiscoveryCandidate{}
	if err := database.First(&member, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if member.CurrentAssessmentID != nil || member.AssessmentState == "ready" {
		t.Fatal("covered member received an independent assessment pointer")
	}
	// A running call which loses representation is retained but must not activate.
	blocked := &blockingDedupeProvider{started: make(chan struct{}), release: make(chan struct{})}
	distinct := create("in-flight", strings.Replace(body, "measured", "sampled", 1))
	var repr material.Representation
	if err := database.Table("material_representations r").Select("r.*").Joins("JOIN material m ON m.current_version_id=r.version_id").Where("m.id=?", distinct.MaterialID).Scan(&repr).Error; err != nil {
		t.Fatal(err)
	}
	// Give the article a standalone current relation, then merge while the call is pending.
	own := dedupe.Member{RunID: run.ID, RepresentationID: repr.ID, MaterialID: distinct.MaterialID, VersionID: repr.VersionID, CandidateID: distinct.ID, RepresentativeID: repr.ID, Similarity: 1, Method: "unique", Revision: 1, Applied: true, UpdatedAt: time.Now()}
	if err := database.Create(&own).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- assessment.AssessMaterial(ctx, distinct.MaterialID, "1", assessment.EnrichmentArticleAssessor{Provider: blocked, Model: config.LLMCHATMODEL})
	}()
	select {
	case <-blocked.started:
	case <-time.After(5 * time.Second):
		t.Fatal("assessment did not start")
	}
	if err := discovery.RunDedupeTask(ctx, jobqueue.DedupeArgs{RepresentationID: repr.ID, Algorithm: dedupe.Algorithm}); err != nil {
		close(blocked.release)
		t.Fatal(err)
	}
	close(blocked.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	member = discovery.DiscoveryCandidate{}
	if err := database.First(&member, distinct.ID).Error; err != nil {
		t.Fatal(err)
	}
	if member.CurrentAssessmentID != nil {
		t.Fatal("in-flight superseded assessment activated")
	}
	var retained int64
	if err := database.Table("discovery_article_assessments").Where("material_id=?", distinct.MaterialID).Count(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if retained != 1 {
		t.Fatal("in-flight result history was lost")
	}
	// Concurrent incremental submissions serialize correctly and preserve direct membership.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- discovery.RunDedupeTask(ctx, jobqueue.DedupeArgs{RepresentationID: repr.ID, Algorithm: dedupe.Algorithm})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var invalid int64
	if err := database.Raw(`SELECT count(*) FROM article_dedupe_members d LEFT JOIN article_dedupe_members r ON r.run_id=d.run_id AND r.representation_id=d.representative_id AND r.representative_id=r.representation_id WHERE d.run_id=? AND d.applied AND r.representation_id IS NULL`, run.ID).Scan(&invalid).Error; err != nil {
		t.Fatal(err)
	}
	if invalid != 0 {
		t.Fatal("chain membership created")
	}
	if _, err := discovery.RequestDedupeAction(ctx, run.ID, "rollback"); err != nil {
		t.Fatal(err)
	}
	waitForPostgresCondition(t, 20*time.Second, func() bool {
		var current dedupe.Run
		return database.First(&current, run.ID).Error == nil && current.Status == "rolled_back"
	})
	member = discovery.DiscoveryCandidate{}
	if err := database.First(&member, second.ID).Error; err != nil {
		t.Fatal(err)
	}
	if member.RepresentativeID != nil {
		t.Fatal("rollback did not restore original projection")
	}
	assertPostgresScalar(t, database, "SELECT to_regclass('public.idx_article_dedupe_bucket')::text", "idx_article_dedupe_bucket")
}

type blockingDedupeProvider struct{ started, release chan struct{} }

func (p *blockingDedupeProvider) AssessArticle(ctx context.Context, input assessment.ChatAssessmentInput) (assessment.ChatAssessmentResult, error) {
	close(p.started)
	select {
	case <-ctx.Done():
		return assessment.ChatAssessmentResult{}, ctx.Err()
	case <-p.release:
	}
	return (&postgresDedupeProvider{}).AssessArticle(ctx, input)
}
