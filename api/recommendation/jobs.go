package recommendation

import (
	"DataArk/jobqueue"
	"context"
	"errors"
	"time"
)

const RecommendationGenerateDailyJobKind = jobqueue.GenerateDailyJobKind

func RunGenerateDailyRecommendationJob(ctx context.Context, userID uint, localDate string) error {
	_, err := GenerateDailyRecommendationsWithReranker(ctx, userID, normalizeRecommendationDate(localDate), ConfiguredRecommendationReranker())
	return err
}

// EnqueueDailyRecommendation preserves the v2 scheduler contract while the
// queue lifecycle is owned by the shared jobqueue package.
func EnqueueDailyRecommendation(ctx context.Context, userID uint, date string) (bool, error) {
	queue, available := jobqueue.Default()
	if !available {
		return false, nil
	}
	if err := queue.EnqueueGenerateDaily(ctx, userID, normalizeRecommendationDate(date)); err != nil {
		return true, err
	}
	return true, nil
}

// RecoverDueJobs enqueues local dates that should already have a daily digest.
// Each user is considered independently so one enqueue failure does not starve
// the remaining users.
func RecoverDueJobs(ctx context.Context, queue jobqueue.JobEnqueuer, now time.Time) error {
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
		var count int64
		if err := db.Model(&RecommendationDay{}).Where("user_id = ? AND recommendation_date = ?", item.UserID, date).Count(&count).Error; err != nil {
			recoveryErrors = append(recoveryErrors, err)
			continue
		}
		if count > 0 {
			continue
		}
		if err := queue.EnqueueGenerateDaily(ctx, item.UserID, date); err != nil {
			recoveryErrors = append(recoveryErrors, err)
		}
	}
	return errors.Join(recoveryErrors...)
}
