package recommendation

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// capturingReranker 记录实际送入模型的候选，用来断言调用前已按上限截断。
type capturingReranker struct {
	got RerankInput
}

func (provider *capturingReranker) Rerank(_ context.Context, input RerankInput) (RerankResult, error) {
	provider.got = input
	items := make([]RerankItem, 0, len(input.Candidates))
	for index, candidate := range input.Candidates {
		items = append(items, RerankItem{
			CandidateID: candidate.CandidateID,
			Rank:        index + 1,
			Reason:      fmt.Sprintf("测试理由 %d", candidate.CandidateID),
			Confidence:  0.5,
		})
	}
	return RerankResult{Items: items, Model: "test-reranker", PromptVersion: "test-v1"}, nil
}

func TestApplyRecommendationRerankerTrimsPoolBeforeCall(t *testing.T) {
	candidates := make([]recommendationCandidateScore, 0, 8)
	for index := 1; index <= 8; index++ {
		candidates = append(candidates, recommendationCandidateScore{
			Candidate: DiscoveryCandidate{ID: uint(index), Title: fmt.Sprintf("候选%d", index)},
		})
	}
	reranker := &capturingReranker{}
	got, model, prompt, err := applyRecommendationReranker(context.Background(), 1, 3, candidates, nil, reranker)
	if err != nil || model != "test-reranker" || prompt != "test-v1" {
		t.Fatalf("unexpected rerank metadata: model=%q prompt=%q err=%v", model, prompt, err)
	}
	if len(reranker.got.Candidates) != 8 {
		t.Fatalf("sent %d candidates, want 8 under default rerank limit", len(reranker.got.Candidates))
	}
	if reranker.got.RequestedCount != 3 || reranker.got.Candidates[0].CandidateID != 1 {
		t.Fatalf("should keep scored prefix: %#v", reranker.got)
	}
	if len(got) != 3 || got[0].Candidate.ID != 1 || got[2].Candidate.ID != 3 || got[0].Reason == "" {
		t.Fatalf("result = %#v", got)
	}
}

func TestApplyRecommendationRerankerRequiresReasonAndProvider(t *testing.T) {
	candidates := []recommendationCandidateScore{{Candidate: DiscoveryCandidate{ID: 1, Title: "A"}}}
	if _, _, _, err := applyRecommendationReranker(context.Background(), 1, 1, candidates, nil, nil); err == nil {
		t.Fatal("nil reranker should fail")
	}
	missingReason := fakeReranker{result: RerankResult{Items: []RerankItem{{CandidateID: 1, Rank: 1, Confidence: 1}}}}
	if _, _, _, err := applyRecommendationReranker(context.Background(), 1, 1, candidates, nil, missingReason); err == nil {
		t.Fatal("missing reason should fail")
	}
	failing := fakeReranker{err: errors.New("down")}
	if _, _, _, err := applyRecommendationReranker(context.Background(), 1, 1, candidates, nil, failing); err == nil {
		t.Fatal("provider error should fail")
	}
}
