package assessment

import (
	"DataArk/discovery"
	"DataArk/llm"
	"math"
	"sort"
	"time"
)

type Metrics struct {
	GeneratedAt     time.Time      `json:"generatedAt"`
	PendingQueue    int64          `json:"pendingQueue"`
	Last24h         MetricsWindow  `json:"last24h"`
	ArticlesPerHour float64        `json:"articlesPerHour"`
	TokenTotals     MetricsTokens  `json:"tokenTotals"`
	Duration        MetricsLatency `json:"duration"`
	SchemaRetryRate float64        `json:"schemaRetryRate"`
}

type MetricsWindow struct {
	Success int64 `json:"success"`
	Failure int64 `json:"failure"`
}

type MetricsTokens struct {
	Prompt     int64 `json:"prompt"`
	Completion int64 `json:"completion"`
	Reasoning  int64 `json:"reasoning"`
	Cached     int64 `json:"cached"`
	Total      int64 `json:"total"`
}

type MetricsLatency struct {
	P50 int64 `json:"p50Ms"`
	P95 int64 `json:"p95Ms"`
}

// GetMetrics 聚合待评估队列与近 24 小时安全 token/耗时，供 owner 评估面板使用。
func GetMetrics(now time.Time) (*Metrics, error) {
	metrics := &Metrics{GeneratedAt: now}
	if db == nil {
		return metrics, nil
	}
	if err := db.Model(&discovery.DiscoveryCandidate{}).
		Where("processing_state = ? AND dedupe_state = ? AND assessment_state = ?", discovery.DiscoveryProcessingReady, discovery.DiscoveryDedupeReady, discovery.DiscoveryAssessmentPending).
		Count(&metrics.PendingQueue).Error; err != nil {
		return nil, err
	}
	since := now.Add(-24 * time.Hour)
	query := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ?", llm.StageArticleAssessment, since)
	if err := query.Where("status = ?", "success").Count(&metrics.Last24h.Success).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ? AND status = ?", llm.StageArticleAssessment, since, "failed").Count(&metrics.Last24h.Failure).Error; err != nil {
		return nil, err
	}
	var articleIDs []uint
	if err := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ? AND status = ? AND candidate_id > 0", llm.StageArticleAssessment, since, "success").Distinct("candidate_id").Pluck("candidate_id", &articleIDs).Error; err != nil {
		return nil, err
	}
	metrics.ArticlesPerHour = float64(len(articleIDs)) / 24
	var totals MetricsTokens
	if err := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ?", llm.StageArticleAssessment, since).
		Select("COALESCE(SUM(prompt_tokens),0) as prompt, COALESCE(SUM(completion_tokens),0) as completion, COALESCE(SUM(reasoning_tokens),0) as reasoning, COALESCE(SUM(cached_tokens),0) as cached, COALESCE(SUM(total_tokens),0) as total").
		Scan(&totals).Error; err != nil {
		return nil, err
	}
	metrics.TokenTotals = totals
	var durations []int64
	if err := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ? AND duration_ms > 0", llm.StageArticleAssessment, since).Pluck("duration_ms", &durations).Error; err != nil {
		return nil, err
	}
	metrics.Duration.P50 = percentileDuration(durations, 50)
	metrics.Duration.P95 = percentileDuration(durations, 95)
	var retried int64
	var assessed int64
	if err := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ?", llm.StageArticleAssessment, since).Count(&assessed).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ? AND attempt > 1", llm.StageArticleAssessment, since).Count(&retried).Error; err != nil {
		return nil, err
	}
	if assessed > 0 {
		metrics.SchemaRetryRate = float64(retried) / float64(assessed)
	}
	return metrics, nil
}

func percentileDuration(values []int64, percentile int) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := int(math.Ceil(float64(percentile) / 100 * float64(len(sorted))))
	if n < 1 {
		n = 1
	}
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[n-1]
}
