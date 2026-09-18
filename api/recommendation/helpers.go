package recommendation

import (
	"DataArk/discovery"
	"encoding/json"
	neturl "net/url"
	"strings"
)

// helpers.go 提供选文与屏蔽共用的小函数。

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

func maxInt(left int, right int) int {
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

// firstNonEmpty 返回第一个去掉空白后非空的字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
