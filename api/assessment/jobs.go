package assessment

import (
	"DataArk/discovery"
	"DataArk/jobqueue"
	"context"
	"errors"
	"strconv"
	"time"
)

// RecoverDueJobs 把已抽取且仍待评估的文章登记到暂停的评估队列，等待 owner 手动执行。
func RecoverDueJobs(ctx context.Context, queue jobqueue.JobEnqueuer, now time.Time) error {
	if db == nil || queue == nil {
		return nil
	}
	query := db.Where("processing_state = ? AND dedupe_state = ? AND assessment_state = ? AND (next_processing_at IS NULL OR next_processing_at <= ?)", discovery.DiscoveryProcessingReady, discovery.DiscoveryDedupeReady, discovery.DiscoveryAssessmentPending, now)
	query = discovery.ExcludeBlacklistedCandidateDomains(query, "")
	var pending []discovery.DiscoveryCandidate
	if err := query.Order("id").Find(&pending).Error; err != nil {
		return err
	}
	var recoverErr error
	for _, candidate := range pending {
		if err := queue.EnqueueAssessArticle(ctx, candidate.ID, strconv.FormatUint(uint64(candidate.ContentVersion), 10)); err != nil {
			recoverErr = errors.Join(recoverErr, err)
		}
	}
	return recoverErr
}
