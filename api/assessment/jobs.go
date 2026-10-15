package assessment

import (
	"DataArk/discovery"
	"DataArk/jobqueue"
	"context"
	"strconv"
	"time"
)

// ArticleJobEnqueuer 只投递文章评估作业，避免评估包依赖全量 jobqueue.JobEnqueuer。
type ArticleJobEnqueuer interface {
	EnqueueAssessArticle(context.Context, uint, string) error
	EnqueueAssessArticles(context.Context, []jobqueue.AssessmentTarget) error
}

// RecoverDueJobs 把已抽取且仍待评估的文章登记到暂停的评估队列，等待 owner 手动执行。
func RecoverDueJobs(ctx context.Context, queue ArticleJobEnqueuer, now time.Time) error {
	if db == nil || queue == nil {
		return nil
	}
	targets, err := discovery.PendingAssessmentTargets(ctx, db, true, &now)
	if err != nil {
		return err
	}
	return queue.EnqueueAssessArticles(ctx, targets)
}

// EnqueuePending 在候选已是 pending 时投递评估作业；装配层在抽取成功后调用。
func EnqueuePending(ctx context.Context, queue ArticleJobEnqueuer, candidateID uint) error {
	if db == nil || queue == nil || candidateID == 0 {
		return nil
	}
	var candidate discovery.DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return err
	}
	qualification, err := discovery.ArticleDedupeQualification(db.WithContext(ctx), candidate.MaterialID)
	if err != nil {
		return err
	}
	if qualification.Active && (!qualification.Ready || !qualification.Representative) {
		return nil
	}
	if !qualification.Active && !qualification.ExactOnly && candidate.RepresentativeID != nil && *candidate.RepresentativeID != candidate.ID {
		return nil
	}
	if candidate.AssessmentState != discovery.DiscoveryAssessmentPending {
		return nil
	}
	return queue.EnqueueAssessArticle(ctx, candidate.MaterialID, strconv.FormatUint(uint64(candidate.ContentVersion), 10))
}
