package recommendation

import (
	"DataArk/config"
	"DataArk/observability"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const digestSummaryItemSummaryRunes = 160

// RecommendationDaySummary is the user-facing digest summary for a single
// recommendation day. Available is false when the day has no published items
// yet; Reason explains why in that case.
type RecommendationDaySummary struct {
	Date          string     `json:"date"`
	Available     bool       `json:"available"`
	Overview      string     `json:"overview"`
	Highlights    []string   `json:"highlights"`
	Topics        []string   `json:"topics"`
	Model         string     `json:"model"`
	PromptVersion string     `json:"promptVersion"`
	GeneratedAt   *time.Time `json:"generatedAt"`
	Reason        string     `json:"reason,omitempty"`
}

// ConfiguredDigestSummaryGenerator mirrors ConfiguredEnrichmentProvider: the
// LLM provider when a chat model is configured, otherwise the rule-based
// generator.
func ConfiguredDigestSummaryGenerator() DigestSummaryGenerator {
	if strings.TrimSpace(config.LLMCHATMODEL) == "" {
		return RuleBasedDigestSummaryGenerator{}
	}
	return configuredOpenAICompatibleProvider()
}

// RuleBasedDigestSummaryGenerator produces a deterministic statistics-based
// summary without any network call. It never fails, so it also serves as the
// fallback when the LLM call fails.
type RuleBasedDigestSummaryGenerator struct{}

func (generator RuleBasedDigestSummaryGenerator) GenerateDigestSummary(_ context.Context, input DigestSummaryInput) (DigestSummaryOutput, error) {
	sourceCounts := make(map[string]int)
	topicCounts := make(map[string]int)
	for _, item := range input.Items {
		if source := strings.TrimSpace(item.Source); source != "" {
			sourceCounts[source]++
		}
		for _, topic := range item.Topics {
			if topic = strings.TrimSpace(topic); topic != "" {
				topicCounts[topic]++
			}
		}
	}
	topTopics := topCountedValues(topicCounts, 3)

	overview := fmt.Sprintf("今日共推荐 %d 篇文章", len(input.Items))
	if len(sourceCounts) > 0 {
		overview += fmt.Sprintf("，覆盖 %d 个来源", len(sourceCounts))
	}
	if len(topTopics) > 0 {
		overview += "，主要主题：" + strings.Join(topTopics, "、")
	}
	overview += "。"

	highlights := make([]string, 0, 3)
	for _, item := range input.Items {
		if len(highlights) == 3 {
			break
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		if source := strings.TrimSpace(item.Source); source != "" {
			highlights = append(highlights, fmt.Sprintf("《%s》（%s）", title, source))
		} else {
			highlights = append(highlights, fmt.Sprintf("《%s》", title))
		}
	}

	return DigestSummaryOutput{
		Overview:      overview,
		Highlights:    highlights,
		Topics:        topTopics,
		Model:         RuleBasedProviderModel,
		PromptVersion: "rule-digest-summary-v1",
	}, nil
}

func topCountedValues(counts map[string]int, limit int) []string {
	type counted struct {
		value string
		count int
	}
	values := make([]counted, 0, len(counts))
	for value, count := range counts {
		values = append(values, counted{value: value, count: count})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].count != values[j].count {
			return values[i].count > values[j].count
		}
		return values[i].value < values[j].value
	})
	top := make([]string, 0, limit)
	for _, value := range values {
		if len(top) == limit {
			break
		}
		top = append(top, value.value)
	}
	return top
}

func GetRecommendationDaySummary(ctx context.Context, userID uint, date string) (*RecommendationDaySummary, error) {
	return GetRecommendationDaySummaryWithGenerator(ctx, userID, date, nil)
}

func GetRecommendationDaySummaryWithGenerator(ctx context.Context, userID uint, date string, generator DigestSummaryGenerator) (*RecommendationDaySummary, error) {
	snapshot, err := GetRecommendationDaySnapshot(userID, date)
	if err != nil {
		return nil, err
	}
	day := snapshot.Day
	date = day.RecommendationDate
	if day.Status != RecommendationDayStatusPublished && day.Status != RecommendationDayStatusSupplemented {
		return &RecommendationDaySummary{Date: date, Highlights: []string{}, Topics: []string{}, Reason: "今日推荐尚未生成"}, nil
	}
	if len(snapshot.Items) == 0 {
		return &RecommendationDaySummary{Date: date, Highlights: []string{}, Topics: []string{}, Reason: "今日推荐暂无文章"}, nil
	}
	if strings.TrimSpace(day.SummaryText) != "" && day.SummaryActualCount == day.ActualCount {
		return daySummaryFromDay(day), nil
	}
	if generator == nil {
		generator = ConfiguredDigestSummaryGenerator()
	}

	input := digestSummaryInputFromSnapshot(snapshot)
	output, generatorErr := generator.GenerateDigestSummary(ctx, input)
	if generatorErr != nil {
		if _, isRuleBased := generator.(RuleBasedDigestSummaryGenerator); !isRuleBased {
			observability.Log(observability.Event{
				Name:         "digest_summary_llm_failed",
				OccurredAt:   time.Now(),
				UserID:       userID,
				DayID:        day.ID,
				LocalDate:    date,
				Status:       "failed",
				ErrorType:    "provider_request",
				ErrorMessage: generatorErr.Error(),
				LLMStage:     llmStageDigestSummary,
			})
			output, generatorErr = RuleBasedDigestSummaryGenerator{}.GenerateDigestSummary(ctx, input)
		}
		if generatorErr != nil {
			return nil, generatorErr
		}
	}

	persistDaySummary(day, userID, output)
	return &RecommendationDaySummary{
		Date:          date,
		Available:     true,
		Overview:      output.Overview,
		Highlights:    nonNilStrings(output.Highlights),
		Topics:        nonNilStrings(output.Topics),
		Model:         output.Model,
		PromptVersion: output.PromptVersion,
		GeneratedAt:   day.SummaryGeneratedAt,
	}, nil
}

func digestSummaryInputFromSnapshot(snapshot *RecommendationDaySnapshot) DigestSummaryInput {
	items := make([]DigestSummaryItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items = append(items, DigestSummaryItem{
			Rank:    item.Rank,
			Title:   item.SnapshotTitle,
			Summary: truncateRunes(item.SnapshotSummary, digestSummaryItemSummaryRunes),
			Source:  item.SnapshotSource,
			Topics:  parseDigestTopicList(item.SnapshotTopics),
			Reason:  item.Reason,
		})
	}
	return DigestSummaryInput{Date: snapshot.Day.RecommendationDate, Items: items}
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func parseDigestTopicList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var topics []string
	if err := json.Unmarshal([]byte(raw), &topics); err == nil {
		cleaned := make([]string, 0, len(topics))
		for _, topic := range topics {
			if topic = strings.TrimSpace(topic); topic != "" {
				cleaned = append(cleaned, topic)
			}
		}
		return cleaned
	}
	parts := strings.Split(raw, ",")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return cleaned
}

// persistDaySummary writes the generated summary with a conditional UPDATE so
// concurrent cold-cache requests converge on a single stored value. A write
// failure never blocks the already generated summary from reaching the user.
func persistDaySummary(day *RecommendationDay, userID uint, output DigestSummaryOutput) {
	if db == nil || day == nil || day.ID == 0 {
		return
	}
	highlightsJSON, err := json.Marshal(nonNilStrings(output.Highlights))
	if err != nil {
		highlightsJSON = []byte("[]")
	}
	topicsJSON, err := json.Marshal(nonNilStrings(output.Topics))
	if err != nil {
		topicsJSON = []byte("[]")
	}
	now := time.Now()
	updates := map[string]interface{}{
		"summary_text":           output.Overview,
		"summary_highlights":     string(highlightsJSON),
		"summary_topics":         string(topicsJSON),
		"summary_model":          output.Model,
		"summary_prompt_version": output.PromptVersion,
		"summary_actual_count":   day.ActualCount,
		"summary_generated_at":   now,
		"updated_at":             now,
	}
	result := db.Model(&RecommendationDay{}).
		Where("id = ? AND user_id = ? AND (summary_text = '' OR summary_actual_count <> actual_count)", day.ID, userID).
		Updates(updates)
	if result.Error != nil {
		return
	}
	if result.RowsAffected == 0 {
		var stored RecommendationDay
		if err := db.Select("summary_text", "summary_highlights", "summary_topics", "summary_model", "summary_prompt_version", "summary_actual_count", "summary_generated_at").
			Where("id = ? AND user_id = ?", day.ID, userID).First(&stored).Error; err != nil {
			return
		}
		*day = mergeDaySummaryFields(*day, stored)
		return
	}
	day.SummaryText = output.Overview
	day.SummaryHighlights = string(highlightsJSON)
	day.SummaryTopics = string(topicsJSON)
	day.SummaryModel = output.Model
	day.SummaryPromptVersion = output.PromptVersion
	day.SummaryActualCount = day.ActualCount
	day.SummaryGeneratedAt = &now
}

func mergeDaySummaryFields(day RecommendationDay, stored RecommendationDay) RecommendationDay {
	day.SummaryText = stored.SummaryText
	day.SummaryHighlights = stored.SummaryHighlights
	day.SummaryTopics = stored.SummaryTopics
	day.SummaryModel = stored.SummaryModel
	day.SummaryPromptVersion = stored.SummaryPromptVersion
	day.SummaryActualCount = stored.SummaryActualCount
	day.SummaryGeneratedAt = stored.SummaryGeneratedAt
	return day
}

func daySummaryFromDay(day *RecommendationDay) *RecommendationDaySummary {
	return &RecommendationDaySummary{
		Date:          day.RecommendationDate,
		Available:     true,
		Overview:      day.SummaryText,
		Highlights:    nonNilStrings(parseDigestTopicList(day.SummaryHighlights)),
		Topics:        nonNilStrings(parseDigestTopicList(day.SummaryTopics)),
		Model:         day.SummaryModel,
		PromptVersion: day.SummaryPromptVersion,
		GeneratedAt:   day.SummaryGeneratedAt,
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
