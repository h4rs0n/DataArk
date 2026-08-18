// 评估模块对外入口：LLM 适配器与作业恢复在本包，落库状态机仍由 discovery 执行以免测试导入环。
package assessment

import (
	"DataArk/discovery"
	"context"
)

type ArticleAssessor = discovery.ArticleAssessor
type ArticleAssessmentInput = discovery.ArticleAssessmentInput
type ArticleAssessmentResult = discovery.ArticleAssessmentResult
type ArticleAssessmentBatchOptions = discovery.ArticleAssessmentBatchOptions
type ArticleAssessmentBatchResult = discovery.ArticleAssessmentBatchResult

func AssessCandidate(ctx context.Context, candidateID uint, enhanced ArticleAssessor) error {
	return discovery.AssessCandidate(ctx, candidateID, enhanced)
}

func PrepareArticleAssessmentBackfill(ctx context.Context, assessor ArticleAssessor, queue discovery.JobEnqueuer, options ArticleAssessmentBatchOptions) (ArticleAssessmentBatchResult, error) {
	return discovery.PrepareArticleAssessmentBackfill(ctx, assessor, queue, options)
}

func RollbackArticleAssessment(ctx context.Context, assessor ArticleAssessor, options ArticleAssessmentBatchOptions) (ArticleAssessmentBatchResult, error) {
	return discovery.RollbackArticleAssessment(ctx, assessor, options)
}
