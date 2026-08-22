package recommendation

import (
	"DataArk/discovery"
	"encoding/json"
	neturl "net/url"
	"strings"
)

// helpers.go 提供选文、屏蔽与评分共用的小函数。

func uintPointer(value uint) *uint {
	return &value
}

func userCandidateStateExcludesRecommendation(state discovery.UserCandidateState) bool {
	return state.OpenedAt != nil || state.ReadAt != nil || state.DeepReadAt != nil || state.ArchivedAt != nil || strings.TrimSpace(state.CurrentFeedback) != ""
}

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
func boundedWeight(value float64) float64 {
	if value > 2 {
		return 2
	}
	if value < -2 {
		return -2
	}
	return value
}
func parseStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func parseWeightMap(raw string) map[string]float64 {
	weights := make(map[string]float64)
	if strings.TrimSpace(raw) == "" {
		return weights
	}
	_ = json.Unmarshal([]byte(raw), &weights)
	return weights
}

func candidateBlocked(candidate DiscoveryCandidate, topics []string, host string, rules []UserBlockRule) bool {
	if len(rules) == 0 {
		return false
	}
	for _, rule := range rules {
		if !rule.Active {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(rule.RuleValue))
		if value == "" {
			continue
		}
		switch rule.RuleType {
		case UserBlockRuleTopic:
			for _, topic := range topics {
				if strings.ToLower(topic) == value {
					return true
				}
			}
		case UserBlockRuleSource:
			if strings.ToLower(candidate.SourceName) == value || strings.ToLower(host) == value {
				return true
			}
		case UserBlockRuleStyle:
			if strings.ToLower(candidate.ContentStyle) == value || strings.ToLower(candidate.ContentType) == value {
				return true
			}
		}
	}
	return false
}

func sourceHost(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func dominantTopicCount(topics []string, counts map[string]int) int {
	maxCount := 0
	for _, topic := range topics {
		if counts[topic] > maxCount {
			maxCount = counts[topic]
		}
	}
	return maxCount
}

func maxSimilarityPenalty(candidate recommendationCandidateScore, selected []recommendationCandidateScore) float64 {
	penalty := 0.0
	for _, item := range selected {
		if firstNonEmpty(candidate.Candidate.SourceName, candidate.SourceHost) == firstNonEmpty(item.Candidate.SourceName, item.SourceHost) {
			penalty = maxFloat(penalty, 0.08)
		}
		if sharedTopic(candidate.Topics, item.Topics) {
			penalty = maxFloat(penalty, 0.12)
		}
		if candidate.Candidate.DuplicateClusterID != "" && candidate.Candidate.DuplicateClusterID == item.Candidate.DuplicateClusterID {
			penalty = maxFloat(penalty, 0.5)
		}
	}
	return penalty
}

func sharedTopic(left []string, right []string) bool {
	seen := make(map[string]struct{})
	for _, value := range left {
		seen[strings.ToLower(value)] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[strings.ToLower(value)]; ok {
			return true
		}
	}
	return false
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxFloat(left float64, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func truncateError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}
