package assessment

import (
	"DataArk/discovery"
	"DataArk/jobqueue"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAssessmentAndRecoveryIgnoreDiscoveryCrawlRestrictions(t *testing.T) {
	setupAssessmentDB(t)
	c := createAssessmentCandidate(t, "Restricted", "https://restricted.example/article", "Article", strings.Repeat("Evidence for the assessment. ", 30), time.Now())
	if _, err := discovery.CreateDiscoveryDomainBlacklist("restricted.example", "fixture"); err != nil {
		t.Fatal(err)
	}
	queue := &recoveryQueue{}
	if err := RecoverDueJobs(context.Background(), queue, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(queue.targets) != 1 || queue.targets[0].MaterialID != c.MaterialID {
		t.Fatalf("restricted assessment not recovered: %+v", queue.targets)
	}
	assessor := fixtureArticleAssessor{name: "crawl-fixture", version: "1", result: ArticleAssessmentResult{Quality: .9, Depth: .8, Evergreen: .7, Confidence: .9, Reasons: []string{"Evidence supports this conclusion"}}}
	if err := AssessMaterial(context.Background(), c.MaterialID, "1", assessor); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&c, c.ID).Error; err != nil {
		t.Fatal(err)
	}
	if c.CurrentAssessmentID == nil || c.EligibilityState != discovery.DiscoveryEligibilityEligible {
		t.Fatalf("restricted assessment not activated: %+v", c)
	}
}

func TestBatchSubmissionAndImportIgnoreCrawlRestrictionChanges(t *testing.T) {
	for _, beforeSubmit := range []bool{true, false} {
		name := "after-submission"
		if beforeSubmit {
			name = "before-submission"
		}
		t.Run(name, func(t *testing.T) {
			p, _, c := setupBatchFixture(t)
			addRule := func() {
				t.Helper()
				if _, err := discovery.CreateDiscoveryDomainBlacklist("batch.invalid", "fixture"); err != nil {
					t.Fatal(err)
				}
			}
			if beforeSubmit {
				addRule()
			}
			if err := StartBatchRun(context.Background()); err != nil && !errors.Is(err, jobqueue.ErrHandlerUnavailable) {
				t.Fatal(err)
			}
			reachSubmittedBatch(t, p)
			if !beforeSubmit {
				addRule()
			}
			var item BatchItem
			if err := db.First(&item).Error; err != nil {
				t.Fatal(err)
			}
			p.output = batchResult(item.CustomID, validBatchAssessment)
			batchStep(t)
			batchStep(t)
			if err := db.First(&item, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&c, c.ID).Error; err != nil {
				t.Fatal(err)
			}
			if item.Status != "succeeded" || c.CurrentAssessmentID == nil || c.EligibilityState != discovery.DiscoveryEligibilityEligible {
				t.Fatalf("restriction blocked Batch activation: %+v %+v", item, c)
			}
		})
	}
}

func TestBatchReimportIgnoresDiscoveryCrawlRestrictions(t *testing.T) {
	p, _, _ := setupBatchFixture(t)
	run, _, item, _ := failedReimportFixture(t, p)
	if _, err := discovery.CreateDiscoveryDomainBlacklist("batch.invalid", "fixture"); err != nil {
		t.Fatal(err)
	}
	result, err := ReimportBatchRun(context.Background(), run.ID, false)
	if err != nil || result.Restored != 1 {
		t.Fatalf("reimport = %+v %v", result, err)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != "succeeded" || item.AssessmentID == nil {
		t.Fatalf("restricted reimport failed: %+v", item)
	}
}
