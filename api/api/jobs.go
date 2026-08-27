package api

import (
	"DataArk/assessment"
	"DataArk/database"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/recommendation"
	"context"
	"errors"
	"time"
)

// jobs.go 装配 River 任务处理器并启动共享队列。

// startApplicationJobQueue 注册发现/评估/日报任务处理器并启动共享队列。
func startApplicationJobQueue(ctx context.Context) (func(), error) {
	ignoreBlacklisted := func(err error) error {
		if errors.Is(err, discovery.ErrDiscoveryDomainBlacklisted) {
			return nil
		}
		return err
	}
	handlers := jobqueue.Handlers{
		FetchSource: func(ctx context.Context, sourceID uint) error {
			return ignoreBlacklisted(discovery.RunFetchDiscoverySourceJob(ctx, sourceID))
		},
		ScanBlogroll: func(ctx context.Context, siteID uint) error {
			return ignoreBlacklisted(discovery.RunScanBlogrollJob(ctx, siteID))
		},
		BackfillSite: func(ctx context.Context, siteID uint) error {
			return ignoreBlacklisted(discovery.RunBackfillSiteJob(ctx, siteID))
		},
		ProcessCandidate: func(ctx context.Context, candidateID uint, contentVersion string) error {
			if err := ignoreBlacklisted(discovery.ProcessCandidate(ctx, candidateID, contentVersion)); err != nil {
				return err
			}
			queue, ok := jobqueue.Default()
			if !ok {
				return nil
			}
			return ignoreBlacklisted(assessment.EnqueuePending(ctx, queue, candidateID))
		},
		AssessArticle: func(ctx context.Context, candidateID uint, contentVersion string) error {
			return ignoreBlacklisted(assessment.AssessCandidate(ctx, candidateID, assessment.ConfiguredArticleAssessor()))
		},
		GenerateDaily:         recommendation.RunGenerateDailyRecommendationJob,
		GenerateDigestSummary: recommendation.RunGenerateDigestSummaryJob,
	}
	recover := func(ctx context.Context, queue jobqueue.JobEnqueuer) error {
		discovery.SetJobQueue(queue)
		now := time.Now()
		return errors.Join(
			discovery.RecoverDueJobs(ctx, queue, now),
			assessment.RecoverDueJobs(ctx, queue, now),
			recommendation.RecoverDueJobs(ctx, queue, now),
		)
	}
	stop, err := jobqueue.Start(ctx, database.DB(), handlers, recover)
	if err != nil {
		discovery.SetJobQueue(nil)
		return nil, err
	}
	if queue, ok := jobqueue.Default(); ok {
		discovery.SetJobQueue(queue)
	}
	return func() {
		discovery.SetJobQueue(nil)
		stop()
	}, nil
}
