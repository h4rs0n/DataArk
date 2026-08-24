package recommendation

import (
	"context"
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
		items = append(items, RerankItem{CandidateID: candidate.CandidateID, Rank: index + 1, Confidence: 0.5})
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
	got, model, prompt, reason := applyRecommendationReranker(context.Background(), 1, 3, candidates, nil, reranker)
	if reason != "" || model != "test-reranker" || prompt != "test-v1" {
		t.Fatalf("unexpected rerank metadata: model=%q prompt=%q reason=%q", model, prompt, reason)
	}
	if len(reranker.got.Candidates) != 3 {
		t.Fatalf("sent %d candidates, want 3", len(reranker.got.Candidates))
	}
	if reranker.got.RequestedCount != 3 || reranker.got.Candidates[0].CandidateID != 1 || reranker.got.Candidates[2].CandidateID != 3 {
		t.Fatalf("should keep the scored prefix: %#v", reranker.got)
	}
	if len(got) != 3 || got[0].Candidate.ID != 1 || got[2].Candidate.ID != 3 {
		t.Fatalf("result = %#v", got)
	}
}
