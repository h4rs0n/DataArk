package assessment

import (
	"DataArk/config"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/llm"
	"context"
	"math"
	"sort"
	"time"
)

type Metrics struct {
	GeneratedAt      time.Time      `json:"generatedAt"`
	PendingQueue     int64          `json:"pendingQueue"`
	Last24h          MetricsWindow  `json:"last24h"`
	ArticlesPerHour  float64        `json:"articlesPerHour"`
	TokenTotals      MetricsTokens  `json:"tokenTotals"`
	Duration         MetricsLatency `json:"duration"`
	SchemaRetryRate  float64        `json:"schemaRetryRate"`
	TokensPerSecond  float64        `json:"tokensPerSecond"`
	AvgJobDurationMs int64          `json:"avgJobDurationMs"`
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

// jobDurationRow 把同一篇文章的多次聊天尝试合成一次作业，用于作业均耗时。
type jobDurationRow struct {
	CandidateID uint  `gorm:"column:candidate_id"`
	DurationMS  int64 `gorm:"column:duration_ms"`
}

// decodeThroughputRow 是单次 chat 的 decode 计时；有 predicted_* 时对齐 llama.cpp eval 速度。
type decodeThroughputRow struct {
	CompletionTokens int   `gorm:"column:completion_tokens"`
	DurationMS       int64 `gorm:"column:duration_ms"`
	PredictedTokens  int   `gorm:"column:predicted_tokens"`
	PredictedMS      int64 `gorm:"column:predicted_ms"`
}

// GetMetrics 聚合待评估队列、完整作业成败、近 24 小时安全 token/耗时，供 owner 评估面板使用。
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
	if err := assignCompleteJobWindow(metrics, since); err != nil {
		return nil, err
	}
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
	if err := assignJobThroughput(metrics, since); err != nil {
		return nil, err
	}
	return metrics, nil
}

// assignCompleteJobWindow 统计近 24 小时完整评估作业的成功/失败；有队列时优先用作业快照。
func assignCompleteJobWindow(metrics *Metrics, since time.Time) error {
	if controller, ok := jobqueue.AssessmentControl(); ok {
		snapshot, err := controller.Snapshot(context.Background(), 1)
		if err == nil && snapshot != nil {
			metrics.Last24h.Success = int64(snapshot.Counts.Succeeded24h)
			metrics.Last24h.Failure = int64(snapshot.Counts.Failed24h)
			return nil
		}
	}
	var successIDs []uint
	if err := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ? AND status = ? AND candidate_id > 0", llm.StageArticleAssessment, since, "success").Distinct("candidate_id").Pluck("candidate_id", &successIDs).Error; err != nil {
		return err
	}
	success := make(map[uint]struct{}, len(successIDs))
	for _, id := range successIDs {
		success[id] = struct{}{}
	}
	metrics.Last24h.Success = int64(len(success))
	var failedIDs []uint
	if err := db.Model(&LLMCall{}).Where("stage = ? AND created_at >= ? AND status = ? AND candidate_id > 0", llm.StageArticleAssessment, since, "failed").Distinct("candidate_id").Pluck("candidate_id", &failedIDs).Error; err != nil {
		return err
	}
	var failures int64
	for _, id := range failedIDs {
		if _, ok := success[id]; !ok {
			failures++
		}
	}
	metrics.Last24h.Failure = failures
	return nil
}

// assignJobThroughput 分开计算 decode token/s 与作业均耗时。
// token/s 按单次 chat 加权：有 llama.cpp timings 用 predicted_n/predicted_ms，否则回退 completion/墙钟。
// 作业均耗时仍按文章汇总墙钟，供队列预计完成使用。
func assignJobThroughput(metrics *Metrics, since time.Time) error {
	var jobRows []jobDurationRow
	if err := db.Model(&LLMCall{}).
		Select("candidate_id, COALESCE(SUM(duration_ms),0) as duration_ms").
		Where("stage = ? AND created_at >= ? AND candidate_id > 0", llm.StageArticleAssessment, since).
		Group("candidate_id").
		Scan(&jobRows).Error; err != nil {
		return err
	}
	var jobDurationMS int64
	var jobCount int64
	for _, row := range jobRows {
		if row.DurationMS <= 0 {
			continue
		}
		jobDurationMS += row.DurationMS
		jobCount++
	}
	if jobCount > 0 {
		metrics.AvgJobDurationMs = jobDurationMS / jobCount
		// articles/hour 由均耗时反推：并发 × 3600 / 单作业秒数，与队列在跑时的产能一致。
		metrics.ArticlesPerHour = float64(assessmentJobConcurrency()) * 3_600_000 / float64(metrics.AvgJobDurationMs)
	}

	var calls []decodeThroughputRow
	if err := db.Model(&LLMCall{}).
		Select("completion_tokens, duration_ms, predicted_tokens, predicted_ms").
		Where("stage = ? AND created_at >= ? AND candidate_id > 0", llm.StageArticleAssessment, since).
		Scan(&calls).Error; err != nil {
		return err
	}
	var outputTokens int64
	var outputDurationMS int64
	for _, call := range calls {
		tokens := int64(call.CompletionTokens)
		durationMS := call.DurationMS
		if call.PredictedTokens > 0 && call.PredictedMS > 0 {
			tokens = int64(call.PredictedTokens)
			durationMS = call.PredictedMS
		}
		if tokens <= 0 || durationMS <= 0 {
			continue
		}
		outputTokens += tokens
		outputDurationMS += durationMS
	}
	if outputDurationMS > 0 {
		metrics.TokensPerSecond = float64(outputTokens) / (float64(outputDurationMS) / 1000)
	}
	return nil
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

// assessmentJobConcurrency 与评估队列工人数、评估槽位默认值一致。
func assessmentJobConcurrency() int {
	if config.ARTICLEASSESSMENTCONCURRENCY < 1 {
		return 2
	}
	return config.ARTICLEASSESSMENTCONCURRENCY
}
