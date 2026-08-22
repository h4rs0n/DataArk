package recommendation

import (
	"context"
	"sort"
	"strings"
)

// rerank.go 调用 reranker 并对候选列表排序裁剪。

func applyRecommendationReranker(ctx context.Context, userID uint, requestedCount int, candidates []recommendationCandidateScore, profile *UserRecommendationProfile, reranker RerankProvider) ([]recommendationCandidateScore, string, string, string) {
	if requestedCount <= 0 {
		requestedCount = 10
	}
	if len(candidates) == 0 {
		return []recommendationCandidateScore{}, "", "", ""
	}
	if reranker == nil {
		return trimRecommendationCandidates(candidates, requestedCount), "", "", ""
	}
	input := RerankInput{
		UserID:          userID,
		RequestedCount:  requestedCount,
		Candidates:      buildRerankCandidates(candidates),
		UserProfileHint: buildUserProfileHint(profile),
	}
	result, err := reranker.Rerank(ctx, input)
	if err != nil {
		return trimRecommendationCandidates(candidates, requestedCount), "", "", "reranker_unavailable: " + truncateError(err.Error(), 300)
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
		candidate, ok := byID[item.CandidateID]
		if !ok {
			continue
		}
		if _, ok := seen[item.CandidateID]; ok {
			continue
		}
		if strings.TrimSpace(item.Reason) != "" {
			candidate.Reason = strings.TrimSpace(item.Reason)
		}
		candidate.RerankScore = clampScore(item.Confidence)
		candidate.RerankRank = item.Rank
		if candidate.RerankRank <= 0 {
			candidate.RerankRank = len(reranked) + 1
		}
		candidate.FinalScore += candidate.RerankScore * 0.05
		reranked = append(reranked, candidate)
		seen[item.CandidateID] = struct{}{}
		if len(reranked) >= requestedCount {
			break
		}
	}
	if len(reranked) == 0 {
		return trimRecommendationCandidates(candidates, requestedCount), "", "", "reranker_invalid_output"
	}
	for _, candidate := range candidates {
		if len(reranked) >= requestedCount {
			break
		}
		if _, ok := seen[candidate.Candidate.ID]; ok {
			continue
		}
		candidate.RerankRank = len(reranked) + 1
		reranked = append(reranked, candidate)
	}
	return reranked, strings.TrimSpace(result.Model), strings.TrimSpace(result.PromptVersion), ""
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
		return ""
	}
	return strings.Join([]string{
		"topics=" + strings.TrimSpace(profile.TopicWeights),
		"styles=" + strings.TrimSpace(profile.StyleWeights),
	}, "\n")
}

func trimRecommendationCandidates(candidates []recommendationCandidateScore, limit int) []recommendationCandidateScore {
	if limit <= 0 || len(candidates) <= limit {
		return candidates
	}
	return candidates[:limit]
}
