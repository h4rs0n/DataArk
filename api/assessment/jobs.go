package assessment

import (
	"DataArk/discovery"
	"context"
	"errors"
	"strconv"
	"time"
)

// ArticleJobEnqueuer 只投递文章评估作业，避免评估包依赖全量 jobqueue.JobEnqueuer。
type ArticleJobEnqueuer interface {
	EnqueueAssessArticle(context.Context, uint, string) error
}

// RecoverDueJobs 把已抽取且仍待评估的文章登记到暂停的评估队列，等待 owner 手动执行。
func RecoverDueJobs(ctx context.Context, queue ArticleJobEnqueuer, now time.Time) error {
	if db == nil || queue == nil {
		return nil
	}
	query := discovery.Candidates(db).Where("processing_state = ? AND dedupe_state = ? AND assessment_state = ? AND (next_processing_at IS NULL OR next_processing_at <= ?)", discovery.DiscoveryProcessingReady, discovery.DiscoveryDedupeReady, discovery.DiscoveryAssessmentPending, now)
	query = discovery.ExcludeBlacklistedCandidateDomains(query, "")
	var pending []discovery.DiscoveryCandidate
	if err := query.Order("id").Find(&pending).Error; err != nil {
		return err
	}
	var recoverErr error
	for _, candidate := range pending {
		if err := queue.EnqueueAssessArticle(ctx, candidate.MaterialID, strconv.FormatUint(uint64(candidate.ContentVersion), 10)); err != nil {
			recoverErr = errors.Join(recoverErr, err)
		}
	}
	return recoverErr
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
	if candidate.AssessmentState != discovery.DiscoveryAssessmentPending {
		return nil
	}
	return queue.EnqueueAssessArticle(ctx, candidate.MaterialID, strconv.FormatUint(uint64(candidate.ContentVersion), 10))
}
