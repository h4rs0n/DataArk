package recommendation

import (
	"DataArk/jobqueue"
	"DataArk/observability"
	"context"
	"errors"
	"strings"
	"time"
)

const RecommendationGenerateDailyJobKind = jobqueue.GenerateDailyJobKind

// DailyJobEnqueuer 只投递日报与摘要作业，避免推荐恢复函数依赖全量 jobqueue.JobEnqueuer。
type DailyJobEnqueuer interface {
	EnqueueGenerateDaily(context.Context, uint, string) error
	EnqueueGenerateDigestSummary(context.Context, uint, string) error
}

func RunGenerateDailyRecommendationJob(ctx context.Context, userID uint, localDate string) error {
	_, err := GenerateDailyRecommendationsWithReranker(ctx, userID, localDate, configuredReranker())
	return err
}

// RunGenerateDigestSummaryJob 为已发布日报预生成用户可见摘要。
func RunGenerateDigestSummaryJob(ctx context.Context, userID uint, localDate string) error {
	_, err := GetRecommendationDaySummaryWithGenerator(ctx, userID, localDate, nil)
	return err
}

// EnqueueDailyRecommendation preserves the v2 scheduler contract while the
// queue lifecycle is owned by the shared jobqueue package.
func EnqueueDailyRecommendation(ctx context.Context, userID uint, date string) (bool, error) {
	queue, available := jobqueue.Default()
	if !available {
		return false, nil
	}
	if strings.TrimSpace(date) == "" {
		var err error
		date, err = RecommendationDateForUser(userID, recommendationClock.Now())
		if err != nil {
			return true, err
		}
	} else {
		date = normalizeRecommendationDate(date)
	}
	if err := queue.EnqueueGenerateDaily(ctx, userID, date); err != nil {
		return true, err
	}
	return true, nil
}

// RecoverDueJobs enqueues local dates that should already have a daily digest.
// Each user is considered independently so one enqueue failure does not starve
// the remaining users. 已发布但摘要缺失或篇数过期的日报会补投摘要作业。
func RecoverDueJobs(ctx context.Context, queue DailyJobEnqueuer, now time.Time) error {
	if db == nil || queue == nil {
		return nil
	}
	var settings []RecommendationSettings
	if err := db.Where("enabled = ?", true).Order("user_id").Find(&settings).Error; err != nil {
		return err
	}
	var recoveryErrors []error
	for _, item := range settings {
		if !recommendationGenerationDue(item, now) {
			continue
		}
		date := recommendationDateForSettings(item, now)
		var day RecommendationDay
		result := db.Where("user_id = ? AND recommendation_date = ?", item.UserID, date).Limit(1).Find(&day)
		if result.Error != nil {
			recoveryErrors = append(recoveryErrors, result.Error)
			continue
		}
		if result.RowsAffected > 0 && (day.Status == RecommendationDayStatusPublished || day.Status == RecommendationDayStatusSupplemented) {
			continue
		}
		if err := queue.EnqueueGenerateDaily(ctx, item.UserID, date); err != nil {
			recoveryErrors = append(recoveryErrors, err)
		}
	}
	if err := recoverStaleDigestSummaries(ctx, queue); err != nil {
		recoveryErrors = append(recoveryErrors, err)
	}
	return errors.Join(recoveryErrors...)
}

// recoverStaleDigestSummaries 把已发布但尚未写出摘要、或补文后篇数对不上的日报重新入队。
func recoverStaleDigestSummaries(ctx context.Context, queue DailyJobEnqueuer) error {
	var days []RecommendationDay
	if err := db.Where(
		"status IN ? AND actual_count > 0 AND (summary_text = '' OR summary_text IS NULL OR summary_actual_count <> actual_count)",
		[]string{RecommendationDayStatusPublished, RecommendationDayStatusSupplemented},
	).Order("id").Find(&days).Error; err != nil {
		return err
	}
	var recoveryErrors []error
	for _, day := range days {
		if err := queue.EnqueueGenerateDigestSummary(ctx, day.UserID, day.RecommendationDate); err != nil {
			recoveryErrors = append(recoveryErrors, err)
		}
	}
	return errors.Join(recoveryErrors...)
}

// dayNeedsDigestSummary 判断已发布日报是否还缺与当前篇数匹配的摘要。
func dayNeedsDigestSummary(day *RecommendationDay) bool {
	if day == nil {
		return false
	}
	if day.Status != RecommendationDayStatusPublished && day.Status != RecommendationDayStatusSupplemented {
		return false
	}
	if day.ActualCount <= 0 {
		return false
	}
	return strings.TrimSpace(day.SummaryText) == "" || day.SummaryActualCount != day.ActualCount
}

// enqueuePublishedDigestSummary 在发布/补文成功后投递摘要作业；无队列或入队失败不阻断主流程。
func enqueuePublishedDigestSummary(ctx context.Context, snapshot *RecommendationDaySnapshot) {
	if snapshot == nil || snapshot.Day == nil || !dayNeedsDigestSummary(snapshot.Day) {
		return
	}
	queue, ok := jobqueue.Default()
	if !ok {
		return
	}
	day := snapshot.Day
	if err := queue.EnqueueGenerateDigestSummary(ctx, day.UserID, day.RecommendationDate); err != nil {
		observability.Log(observability.Event{
			Name:         "digest_summary_enqueue_failed",
			OccurredAt:   time.Now(),
			UserID:       day.UserID,
			DayID:        day.ID,
			LocalDate:    day.RecommendationDate,
			Status:       "failed",
			ErrorType:    "enqueue",
			ErrorMessage: err.Error(),
		})
	}
}
