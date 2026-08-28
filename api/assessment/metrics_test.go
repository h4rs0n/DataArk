package assessment

import (
	"DataArk/config"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/llm"
	"DataArk/observability"
	"testing"
	"time"
)

func TestGetMetricsAggregatesQueueAndSafeTokenRows(t *testing.T) {
	setupAssessmentDB(t)
	restoreAssessmentConcurrency(t, 2)
	now := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	stubAssessmentSnapshot(t, runningAssessmentSnapshot(now.Add(-2*time.Hour), 1, 2, 1))
	rows := []LLMCall{
		{CandidateID: 7, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 1, DurationMS: 100, PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CreatedAt: now.Add(-time.Hour)},
		{CandidateID: 8, Stage: llm.StageArticleAssessment, Status: "failed", Attempt: 1, DurationMS: 50, CompletionTokens: 2, ErrorType: "invalid_output", CreatedAt: now.Add(-time.Hour)},
		{CandidateID: 8, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 2, DurationMS: 400, PromptTokens: 20, CompletionTokens: 6, TotalTokens: 26, CreatedAt: now.Add(-time.Hour)},
		{CandidateID: 9, Stage: llm.StageArticleAssessment, Status: "failed", Attempt: 1, DurationMS: 50, ErrorType: "invalid_output", CreatedAt: now.Add(-time.Hour)},
		{CandidateID: 10, Stage: llm.StageRecommendationRerank, Status: "success", Attempt: 1, DurationMS: 10, PromptTokens: 99, CreatedAt: now.Add(-time.Hour)},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	metrics, err := GetMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.PendingQueue != 1 {
		t.Fatalf("pendingQueue = %d", metrics.PendingQueue)
	}
	if metrics.CurrentRun.Success != 2 || metrics.CurrentRun.Failure != 1 {
		t.Fatalf("currentRun = %#v", metrics.CurrentRun)
	}
	if metrics.TokenTotals.Prompt != 30 || metrics.TokenTotals.Total != 41 {
		t.Fatalf("tokens = %#v", metrics.TokenTotals)
	}
	if metrics.Duration.P50 != 50 || metrics.Duration.P95 != 400 {
		t.Fatalf("duration = %#v", metrics.Duration)
	}
	if metrics.SchemaRetryRate < 0.2 || metrics.SchemaRetryRate > 0.3 {
		t.Fatalf("schemaRetryRate = %v", metrics.SchemaRetryRate)
	}
	// 候选 7/8 有输出：token/s = (5+2+6) / ((100+50+400)/1000) = 23.636...
	if metrics.TokensPerSecond < 23.6 || metrics.TokensPerSecond > 23.7 {
		t.Fatalf("tokensPerSecond = %v", metrics.TokensPerSecond)
	}
	if metrics.AvgJobDurationMs != 200 {
		t.Fatalf("avgJobDurationMs = %d", metrics.AvgJobDurationMs)
	}
	// 均耗时反推：2 并发 × 3600s / 0.2s = 36000
	if metrics.ArticlesPerHour != 36000 {
		t.Fatalf("articlesPerHour = %v", metrics.ArticlesPerHour)
	}
}

func TestGetMetricsPrefersLlamaCppDecodeTimings(t *testing.T) {
	setupAssessmentDB(t)
	restoreAssessmentConcurrency(t, 2)
	now := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	stubAssessmentSnapshot(t, runningAssessmentSnapshot(now.Add(-2*time.Hour), 0, 0, 0))
	rows := []LLMCall{
		{CandidateID: 11, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 1, DurationMS: 9000, CompletionTokens: 10, PredictedTokens: 100, PredictedMS: 2000, CreatedAt: now.Add(-time.Hour)},
		{CandidateID: 12, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 1, DurationMS: 1000, CompletionTokens: 10, CreatedAt: now.Add(-time.Hour)},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	metrics, err := GetMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	// 有 timings 的行用 100/2s，无 timings 的行用 10/1s：token/s = (100+10) / 3 = 36.666...
	if metrics.TokensPerSecond < 36.6 || metrics.TokensPerSecond > 36.7 {
		t.Fatalf("tokensPerSecond = %v", metrics.TokensPerSecond)
	}
	if metrics.AvgJobDurationMs != 5000 {
		t.Fatalf("avgJobDurationMs = %d", metrics.AvgJobDurationMs)
	}
	if metrics.ArticlesPerHour != 1440 {
		t.Fatalf("articlesPerHour = %v", metrics.ArticlesPerHour)
	}
}

func TestPersistLLMCallEventWritesDecodeTimings(t *testing.T) {
	setupAssessmentDB(t)
	persistLLMCallEvent(observability.Event{
		CandidateID: 3, LLMStage: llm.StageArticleAssessment, Status: "success",
		LLMDuration: 9000,
		LLMUsage: &observability.LLMUsage{
			Available: true, CompletionTokens: 10, PredictedTokens: 100, PredictedMS: 2000,
		},
	})
	var row LLMCall
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.PredictedTokens != 100 || row.PredictedMS != 2000 || row.CompletionTokens != 10 {
		t.Fatalf("persisted llm call = %#v", row)
	}
}

func TestGetMetricsClearsWindowWhenQueueIsIdle(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	stubAssessmentSnapshot(t, &jobqueue.CrawlQueueSnapshot{
		State:  "waiting",
		Counts: jobqueue.CrawlQueueCounts{Pending: 1},
	})
	if err := db.Create(&LLMCall{
		CandidateID: 21, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 1,
		DurationMS: 1000, CompletionTokens: 10, CreatedAt: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	metrics, err := GetMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.PendingQueue != 1 {
		t.Fatalf("pendingQueue = %d", metrics.PendingQueue)
	}
	if metrics.CurrentRun.Success != 0 || metrics.CurrentRun.Failure != 0 || metrics.TokenTotals.Total != 0 || metrics.TokensPerSecond != 0 || metrics.ArticlesPerHour != 0 || metrics.AvgJobDurationMs != 0 {
		t.Fatalf("idle metrics = %#v", metrics)
	}
}

func TestGetMetricsIgnoresCallsBeforeCurrentRun(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	stubAssessmentSnapshot(t, runningAssessmentSnapshot(now.Add(-10*time.Minute), 0, 1, 0))
	if err := db.Create(&LLMCall{
		CandidateID: 1, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 1,
		DurationMS: 1000, CompletionTokens: 10, CreatedAt: now.Add(-time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&LLMCall{
		CandidateID: 2, Stage: llm.StageArticleAssessment, Status: "success", Attempt: 1,
		DurationMS: 1000, CompletionTokens: 4, CreatedAt: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	metrics, err := GetMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.CurrentRun.Success != 1 || metrics.TokenTotals.Completion != 4 {
		t.Fatalf("current-run metrics = %#v", metrics)
	}
}

func TestGetMetricsPendingQueueIgnoresCandidateRowsWithoutSnapshot(t *testing.T) {
	setupAssessmentDB(t)
	now := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	pending := discovery.DiscoveryCandidate{
		SourceID: 1, SourceName: "Feed", URL: "https://metrics.example/no-snapshot",
		Status: discovery.DiscoveryCandidateStatusNew, ProcessingState: discovery.DiscoveryProcessingReady,
		DedupeState: discovery.DiscoveryDedupeReady, AssessmentState: discovery.DiscoveryAssessmentPending,
		ContentVersion: 1, LastSeenAt: now,
	}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	metrics, err := GetMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.PendingQueue != 0 {
		t.Fatalf("pendingQueue = %d, want 0 without snapshot", metrics.PendingQueue)
	}
}

func TestGetMetricsPendingQueueWorksWhenDBNil(t *testing.T) {
	previous := db
	db = nil
	t.Cleanup(func() { db = previous })
	now := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	stubAssessmentSnapshot(t, &jobqueue.CrawlQueueSnapshot{
		State:  "waiting",
		Counts: jobqueue.CrawlQueueCounts{Pending: 4},
	})
	metrics, err := GetMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.PendingQueue != 4 {
		t.Fatalf("pendingQueue = %d", metrics.PendingQueue)
	}
	if metrics.CurrentRun.Success != 0 || metrics.TokenTotals.Total != 0 {
		t.Fatalf("nil-db metrics = %#v", metrics)
	}
}

// stubAssessmentSnapshot 注入评估队列快照，供 GetMetrics 测试使用。
func stubAssessmentSnapshot(t *testing.T, snapshot *jobqueue.CrawlQueueSnapshot) {
	t.Helper()
	previous := assessmentSnapshot
	assessmentSnapshot = func() (*jobqueue.CrawlQueueSnapshot, error) { return snapshot, nil }
	t.Cleanup(func() { assessmentSnapshot = previous })
}

// runningAssessmentSnapshot 构造一次正在执行的手动评估快照。
func runningAssessmentSnapshot(startedAt time.Time, pending, succeeded, failed int) *jobqueue.CrawlQueueSnapshot {
	started := startedAt
	return &jobqueue.CrawlQueueSnapshot{
		State:        "running",
		RunStartedAt: &started,
		Counts: jobqueue.CrawlQueueCounts{
			Pending:      pending,
			Succeeded24h: succeeded,
			Failed24h:    failed,
		},
	}
}

func restoreAssessmentConcurrency(t *testing.T, concurrency int) {
	t.Helper()
	previous := config.ARTICLEASSESSMENTCONCURRENCY
	config.ARTICLEASSESSMENTCONCURRENCY = concurrency
	t.Cleanup(func() { config.ARTICLEASSESSMENTCONCURRENCY = previous })
}
