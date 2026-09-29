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

// assessmentRecoveryColumns 只含入队所需字段；候选投影整行读取会拖上数百 MB 的 body_text。
var assessmentRecoveryColumns = []string{"id", "material_id", "content_version"}

// RecoverDueJobs 把已抽取且仍待评估的文章登记到暂停的评估队列，等待 owner 手动执行。
func RecoverDueJobs(ctx context.Context, queue ArticleJobEnqueuer, now time.Time) error {
	if db == nil || queue == nil {
		return nil
	}
	query := discovery.Candidates(db).Select(assessmentRecoveryColumns).Where("processing_state = ? AND dedupe_state = ? AND assessment_state = ? AND (next_processing_at IS NULL OR next_processing_at <= ?)", discovery.DiscoveryProcessingReady, discovery.DiscoveryDedupeReady, discovery.DiscoveryAssessmentPending, now)
	query = discovery.ExcludeBlacklistedCandidateDomains(query, "")
	var pending []discovery.DiscoveryCandidate
	if err := query.Order("id").Find(&pending).Error; err != nil {
		return err
	}
	targets := make([]jobqueue.AssessmentTarget, 0, len(pending))
	for _, candidate := range pending {
		targets = append(targets, jobqueue.AssessmentTarget{MaterialID: candidate.MaterialID, ContentVersion: strconv.FormatUint(uint64(candidate.ContentVersion), 10)})
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
	if candidate.AssessmentState != discovery.DiscoveryAssessmentPending {
		return nil
	}
	return queue.EnqueueAssessArticle(ctx, candidate.MaterialID, strconv.FormatUint(uint64(candidate.ContentVersion), 10))
}
