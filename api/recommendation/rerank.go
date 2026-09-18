package recommendation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"DataArk/config"
)

// rerank.go 强制调用 reranker，模型顺序就是最终名次。

func applyRecommendationReranker(ctx context.Context, userID uint, requestedCount int, candidates []recommendationCandidateScore, profile *UserRecommendationProfile, reranker RerankProvider) ([]recommendationCandidateScore, string, string, error) {
	if requestedCount <= 0 {
		requestedCount = 10
	}
	if len(candidates) == 0 {
		return []recommendationCandidateScore{}, "", "", nil
	}
	if reranker == nil {
		return nil, "", "", errors.New("recommendation reranker is required")
	}
	poolLimit := config.RECOMMENDATIONRERANKLIMIT
	if poolLimit < requestedCount {
		poolLimit = requestedCount
	}
	candidates = trimRecommendationCandidates(candidates, poolLimit)
	input := RerankInput{
		UserID:          userID,
		RequestedCount:  requestedCount,
		Candidates:      buildRerankCandidates(candidates),
		UserProfileHint: buildUserProfileHint(profile),
	}
	result, err := reranker.Rerank(ctx, input)
	if err != nil {
		return nil, "", "", fmt.Errorf("reranker_unavailable: %w", err)
	}
	byID := make(map[uint]recommendationCandidateScore, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.Candidate.ID] = candidate
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		if result.Items[i].Rank != result.Items[j].Rank {
			return result.Items[i].Rank < result.Items[j].Rank
		}
		return i < j
	})
	seen := make(map[uint]struct{})
	reranked := make([]recommendationCandidateScore, 0, requestedCount)
	for _, item := range result.Items {
		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			continue
		}
		candidate, ok := byID[item.CandidateID]
		if !ok {
			continue
		}
		if _, ok := seen[item.CandidateID]; ok {
			continue
		}
		candidate.Reason = reason
		candidate.RerankScore = clampScore(item.Confidence)
		candidate.RerankRank = item.Rank
		if candidate.RerankRank <= 0 {
			candidate.RerankRank = len(reranked) + 1
		}
		candidate.FinalScore = candidate.RerankScore
		reranked = append(reranked, candidate)
		seen[item.CandidateID] = struct{}{}
		if len(reranked) >= requestedCount {
			break
		}
	}
	if len(reranked) == 0 {
		return nil, "", "", errors.New("reranker_invalid_output")
	}
	return reranked, strings.TrimSpace(result.Model), strings.TrimSpace(result.PromptVersion), nil
}

func buildRerankCandidates(candidates []recommendationCandidateScore) []RerankCandidate {
	items := make([]RerankCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, RerankCandidate{
			CandidateID:  candidate.Candidate.ID,
			Title:        candidate.Candidate.Title,
			Summary:      candidate.Candidate.Summary,
			Topics:       candidate.Topics,
			Source:       firstNonEmpty(candidate.Candidate.SourceName, candidate.SourceHost),
			PublishedAt:  candidate.Candidate.PublishedAt,
			QualityScore: candidate.Candidate.QualityScore,
			DepthScore:   candidate.Candidate.DepthScore,
		})
	}
	return items
}

func buildUserProfileHint(profile *UserRecommendationProfile) string {
	if profile == nil {
		return "Prefer source and topic diversity. Include some exploration when the user's history is concentrated. Every item needs a Chinese reason."
	}
	return strings.Join([]string{
		"topics=" + strings.TrimSpace(profile.TopicWeights),
		"styles=" + strings.TrimSpace(profile.StyleWeights),
		fmt.Sprintf("explorationRate=%.2f", profile.ExplorationRate),
		"Prefer source and topic diversity. Include some exploration when the user's history is concentrated. Every item needs a Chinese reason.",
	}, "\n")
}

func trimRecommendationCandidates(candidates []recommendationCandidateScore, limit int) []recommendationCandidateScore {
	if limit <= 0 || len(candidates) <= limit {
		return candidates
	}
	return candidates[:limit]
}
