package recommendation

import (
	"context"
	"strings"
)

const RuleBasedProviderModel = "rule-based"

type RuleBasedEnrichmentProvider struct {
	PromptVersion string
}

func (provider RuleBasedEnrichmentProvider) Enrich(_ context.Context, input EnrichmentInput) (EnrichmentResult, error) {
	text := strings.Join(strings.Fields(input.Title+" "+input.Summary+" "+input.BodyText), " ")
	topics := inferTopics(text)
	contentType, contentStyle := inferContentShape(text)
	quality := 0.5
	if strings.TrimSpace(input.Title) != "" {
		quality += 0.15
	}
	if len([]rune(strings.TrimSpace(input.BodyText))) > 800 {
		quality += 0.2
	}
	if len(topics) > 0 {
		quality += 0.1
	}
	depth := 0.4
	if strings.Contains(strings.ToLower(text), "tutorial") || strings.Contains(strings.ToLower(text), "guide") || strings.Contains(text, "教程") {
		depth += 0.25
	}
	if len([]rune(strings.TrimSpace(input.BodyText))) > 1600 {
		depth += 0.25
	}
	return EnrichmentResult{
		Summary:       firstNonEmpty(input.Summary, input.Title),
		Topics:        topics,
		Entities:      topics,
		ContentType:   contentType,
		ContentStyle:  contentStyle,
		Language:      inferLanguage(text),
		QualityScore:  clamp(quality),
		DepthScore:    clamp(depth),
		Model:         RuleBasedProviderModel,
		PromptVersion: firstNonEmpty(provider.PromptVersion, "rule-v1"),
	}, nil
}

func inferTopics(text string) []string {
	lower := strings.ToLower(text)
	candidates := []struct {
		token string
		topic string
	}{
		{"postgres", "PostgreSQL"},
		{"pgvector", "pgvector"},
		{"kubernetes", "Kubernetes"},
		{"llm", "LLM"},
		{"embedding", "Embedding"},
		{"golang", "Go"},
		{" go ", "Go"},
		{"rss", "RSS"},
	}
	topics := make([]string, 0)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		if strings.Contains(lower, candidate.token) {
			if _, ok := seen[candidate.topic]; !ok {
				topics = append(topics, candidate.topic)
				seen[candidate.topic] = struct{}{}
			}
		}
	}
	return topics
}

func inferContentShape(text string) (string, string) {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "tutorial") || strings.Contains(lower, "guide") || strings.Contains(text, "教程"):
		return "tutorial", "technical_deep_dive"
	case strings.Contains(lower, "release") || strings.Contains(text, "发布"):
		return "release", "news"
	default:
		return "article", "general"
	}
}

func inferLanguage(text string) string {
	for _, value := range text {
		if value >= '\u4e00' && value <= '\u9fff' {
			return "zh-CN"
		}
	}
	return "en"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
