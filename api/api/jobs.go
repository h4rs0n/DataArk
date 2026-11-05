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
	"log"
	"strconv"
	"sync"
	"time"
)

// jobs.go 装配 River 任务处理器并启动共享队列。

// startApplicationJobQueue 注册发现/评估/日报任务处理器并启动共享队列。
func startApplicationJobQueue(ctx context.Context) (func(), error) {
	if err := assessment.InitializeBatchRuntime(ctx); err != nil {
		log.Printf("assessment Batch unavailable: %v", err)
	}
	ignoreBlacklisted := func(err error) error {
		if errors.Is(err, discovery.ErrDiscoveryDomainBlacklisted) {
			return nil
		}
		return err
	}
	handlers := jobqueue.Handlers{
		BatchTick:           assessment.ProcessBatchTick,
		ReservedAssessments: assessment.ReservedBatchTargets,
		Dedupe:              discovery.RunDedupeTask,
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
			reserved, err := assessment.BatchActiveTarget(database.DB().WithContext(ctx), materialID, contentVersion)
			if err != nil {
				return err
			}
			if reserved {
				return nil
			}
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
	recoveryCtx, cancelRecovery := context.WithCancel(ctx)
	var recoveryWorkers sync.WaitGroup
	recover := func(ctx context.Context, queue jobqueue.JobEnqueuer) error {
		discovery.SetJobQueue(queue)
		recoveries := []moduleRecovery{
			{name: "discovery", run: func(ctx context.Context) error { return discovery.RecoverDueJobs(ctx, queue, time.Now()) }},
			{name: "dedupe", run: discovery.RecoverDedupeJobs, externalRetry: true},
			{name: "assessment", run: func(ctx context.Context) error {
				return assessment.RecoverDueJobs(ctx, queue, time.Now())
			}},
			{name: "recommendation", run: func(ctx context.Context) error { return recommendation.RecoverDueJobs(ctx, queue, time.Now()) }},
			{name: "batch", run: assessment.RecoverBatchJobs, periodic: true},
		}
		startModuleRecoveries(recoveryCtx, recoveries, &recoveryWorkers)
		return nil
	}
	stop, err := jobqueue.Start(ctx, database.DB(), handlers, recover)
	if err != nil {
		cancelRecovery()
		recoveryWorkers.Wait()
		discovery.SetJobQueue(nil)
		return nil, err
	}
	if queue, ok := jobqueue.Default(); ok {
		discovery.SetJobQueue(queue)
	}
	stopDedupeRecovery := discovery.StartDedupeRecovery(ctx)
	return func() {
		cancelRecovery()
		recoveryWorkers.Wait()
		stopDedupeRecovery()
		discovery.SetJobQueue(nil)
		stop()
	}, nil
}

// moduleRecovery errors never tear down unrelated workers. Only failed startup
// recovery is retried, except Batch which also schedules durable future ticks.
type moduleRecovery struct {
	name          string
	run           func(context.Context) error
	periodic      bool
	externalRetry bool // Dedupe already has its own bounded periodic recovery.
}

func (recovery moduleRecovery) recover(ctx context.Context) bool {
	if err := recovery.run(ctx); err != nil {
		log.Printf("job recovery module=%s failed: %v", recovery.name, err)
		return true
	}
	return false
}
func (recovery moduleRecovery) retry(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !recovery.recover(ctx) && !recovery.periodic {
				return
			}
		}
	}
}

func startModuleRecoveries(ctx context.Context, recoveries []moduleRecovery, workers *sync.WaitGroup) {
	for _, recovery := range recoveries {
		retry := recovery.recover(ctx)
		if (retry && !recovery.externalRetry) || recovery.periodic {
			workers.Add(1)
			go func(recovery moduleRecovery) { defer workers.Done(); recovery.retry(ctx) }(recovery)
		}
	}
}
