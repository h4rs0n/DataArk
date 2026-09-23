package api

import (
	"DataArk/assessment"
	"DataArk/database"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/material"
	"DataArk/recommendation"
	"context"
	"errors"
	"strconv"
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
		AssessArticle: func(ctx context.Context, materialID uint, contentVersion string) error {
			return ignoreBlacklisted(assessment.AssessMaterial(ctx, materialID, contentVersion, assessment.ConfiguredArticleAssessor()))
		},
		AssessLegacyCandidate: func(ctx context.Context, candidateID uint, contentVersion string) error {
			var mapping material.CandidateVersion
			result := database.DB().Where("candidate_id = ? AND content_version = ?", candidateID, contentVersion).Limit(1).Find(&mapping)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return nil
			}
			var version material.Version
			if err := database.DB().First(&version, mapping.VersionID).Error; err != nil {
				return err
			}
			return assessment.AssessMaterial(ctx, version.MaterialID, strconv.FormatUint(uint64(version.Version), 10), assessment.ConfiguredArticleAssessor())
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
