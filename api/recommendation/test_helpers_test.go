package recommendation

import (
	"DataArk/assessment"
	"DataArk/discovery"
	"context"
	"fmt"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// identityReranker 保持候选顺序并补 reason，供 SQLite 单测走强制 rerank 成功路。
type identityReranker struct{}

func (identityReranker) Rerank(_ context.Context, input RerankInput) (RerankResult, error) {
	limit := input.RequestedCount
	if limit <= 0 {
		limit = len(input.Candidates)
	}
	items := make([]RerankItem, 0, limit)
	for index, candidate := range input.Candidates {
		if index >= limit {
			break
		}
		reason := fmt.Sprintf("测试重排：%s", firstNonEmpty(candidate.Title, fmt.Sprintf("候选%d", candidate.CandidateID)))
		items = append(items, RerankItem{
			CandidateID: candidate.CandidateID,
			Rank:        index + 1,
			Reason:      reason,
			Confidence:  clampScore(candidate.QualityScore),
		})
	}
	return RerankResult{Items: items, Model: "test-identity", PromptVersion: "test-identity-v1"}, nil
}

// identityDigestSummaryGenerator 为测试提供确定性 LLM 摘要替身。
type identityDigestSummaryGenerator struct{}

func (identityDigestSummaryGenerator) GenerateDigestSummary(_ context.Context, input DigestSummaryInput) (DigestSummaryOutput, error) {
	highlights := make([]string, 0, 3)
	topics := make([]string, 0)
	seenTopic := make(map[string]struct{})
	for _, item := range input.Items {
		if len(highlights) < 3 && item.Title != "" {
			highlights = append(highlights, item.Title)
		}
		for _, topic := range item.Topics {
			if _, ok := seenTopic[topic]; ok {
				continue
			}
			seenTopic[topic] = struct{}{}
			topics = append(topics, topic)
		}
	}
	return DigestSummaryOutput{
		Overview:      fmt.Sprintf("测试摘要：共 %d 篇。", len(input.Items)),
		Highlights:    highlights,
		Topics:        topics,
		Model:         "test-digest",
		PromptVersion: "test-digest-v1",
	}, nil
}

func setupSQLiteDB(t *testing.T) {
	t.Helper()
	sqliteDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := sqliteDB.AutoMigrate(
		&discovery.DiscoverySource{},
		&discovery.DiscoveryCandidate{},
		&discovery.DiscoveryCandidateFeedback{},
		&discovery.DiscoveryDuplicateReviewSignal{},
		&assessment.ArticleAssessment{},
		&discovery.UserCandidateState{},
		&discovery.DiscoverySite{},
		&discovery.DiscoverySiteEdge{},
		&discovery.DiscoveryCandidateProvenance{},
		&discovery.DiscoveryFetchRun{},
		&discovery.DiscoveryBackfillState{},
		&discovery.DiscoverySiteOperationalStats{},
		&discovery.DiscoverySourceScheduleDecision{},
		&discovery.DiscoveryDomainBlacklistEntry{},
		&RecommendationSettings{},
		&RecommendationDay{},
		&RecommendationFeedBatch{},
		&RecommendationItem{},
		&RecommendationFeedback{},
		&UserBlockRule{},
		&UserRecommendationProfile{},
	); err != nil {
		t.Fatalf("failed to migrate sqlite db: %v", err)
	}
	oldDiscovery := discovery.SetDB(sqliteDB)
	oldAssessment := assessment.SetDB(sqliteDB)
	oldRecommendation := SetDB(sqliteDB)
	oldReranker := configuredReranker
	configuredReranker = func() RerankProvider { return identityReranker{} }
	oldSummary := configuredDigestSummaryGenerator
	configuredDigestSummaryGenerator = func() DigestSummaryGenerator { return identityDigestSummaryGenerator{} }
	t.Cleanup(func() {
		discovery.SetDB(oldDiscovery)
		assessment.SetDB(oldAssessment)
		SetDB(oldRecommendation)
		configuredReranker = oldReranker
		configuredDigestSummaryGenerator = oldSummary
	})
}
