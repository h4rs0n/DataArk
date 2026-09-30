package api

import (
	"DataArk/archive"
	"DataArk/assessment"
	"DataArk/auth"
	"DataArk/backup"
	"DataArk/bootstrap"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/recommendation"
	"DataArk/search"
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
)

// deps.go 保存可在测试中替换的未导出函数变量。

var (
	checkArchiveConsistency            = search.CheckArchiveConsistency
	repairArchiveConsistency           = search.RepairArchiveConsistency
	registerWithToken                  = auth.RegisterWithToken
	loginWithToken                     = auth.LoginWithToken
	queryByKeyword                     = search.QueryByKeyword
	addDocURLTask                      = search.AddDocURLTask
	getArchiveTask                     = search.GetArchiveTask
	getArchiveStatsSnapshot            = archive.GetArchiveStats
	refreshStatsFromDisk               = archive.RefreshArchiveStatsFromDisk
	recordSearchEvent                  = archive.RecordSearchEvent
	getKeywordStats                    = archive.GetKeywordStats
	recordArchiveClick                 = archive.RecordArchiveClick
	getArchiveRankings                 = archive.GetArchiveRankings
	getArchiveRecommendations          = archive.GetArchiveRecommendations
	listDiscoverySources               = discovery.ListDiscoverySources
	listDiscoveryDomainBlacklist       = discovery.ListDiscoveryDomainBlacklist
	createDiscoveryDomainBlacklist     = discovery.CreateDiscoveryDomainBlacklist
	deleteDiscoveryDomainBlacklist     = discovery.DeleteDiscoveryDomainBlacklist
	createDiscoverySource              = discovery.CreateDiscoverySource
	updateDiscoverySource              = discovery.UpdateDiscoverySource
	deleteDiscoverySource              = discovery.DeleteDiscoverySource
	fetchDiscoverySourceByID           = discovery.FetchDiscoverySourceByID
	getDiscoverySiteGraph              = discovery.GetSiteGraph
	listBackfillCoverage               = discovery.ListBackfillCoverage
	updateDiscoverySiteStatus          = discovery.UpdateDiscoverySiteOperationalStatus
	requestDiscoverySiteBackfill       = discovery.RequestDiscoverySiteBackfill
	getDiscoverySiteOperations         = discovery.GetDiscoverySiteOperations
	getDiscoveryCandidate              = discovery.GetDiscoveryCandidate
	markCandidateRead                  = discovery.MarkUserCandidateRead
	markCandidateArchived              = discovery.MarkUserCandidateArchived
	getRecommendationSettings          = recommendation.GetRecommendationSettings
	saveRecommendationSettings         = recommendation.SaveRecommendationSettings
	getRecommendationDaySnapshot       = recommendation.GetRecommendationDaySnapshot
	getRecommendationDaySummary        = recommendation.GetRecommendationDaySummary
	getCurrentDiscoveryFeed            = recommendation.GetCurrentDiscoveryFeed
	refreshDiscoveryFeed               = recommendation.RefreshDiscoveryFeed
	recommendationDateForUser          = recommendation.RecommendationDateForUser
	recommendationNow                  = time.Now
	listRecommendationDays             = recommendation.ListRecommendationDays
	createRecommendationDay            = recommendation.CreateRecommendationDay
	generateDailyRecommendations       = recommendation.GenerateDailyRecommendations
	regenerateRecommendations          = recommendation.RegenerateDailyRecommendations
	supplementRecommendations          = recommendation.SupplementDailyRecommendations
	recordRecommendationFeedback       = recommendation.RecordRecommendationFeedback
	revertRecommendationFeedback       = recommendation.RevertRecommendationFeedback
	getCurrentRecommendationFeedback   = recommendation.GetCurrentRecommendationFeedback
	getRecommendationItemContext       = recommendation.GetRecommendationItemContext
	listRecommendationFeedbackHistory  = recommendation.ListRecommendationFeedbackHistory
	resetUserRecommendationPreferences = recommendation.ResetUserRecommendationPreferences
	listUserBlockRules                 = recommendation.ListUserBlockRules
	deleteUserBlockRule                = recommendation.DeleteUserBlockRule
	getCandidateInventory              = recommendation.GetCandidateInventory
	getAdminProductMetrics             = recommendation.GetAdminProductMetrics
	getAssessmentMetrics               = assessment.GetMetrics
	getDiscoveryCrawlQueue             = func(ctx context.Context, limit int) (*jobqueue.CrawlQueueSnapshot, error) {
		controller, available := jobqueue.CrawlControl()
		if !available {
			return nil, errors.New("discovery crawl queue is unavailable")
		}
		return controller.Snapshot(ctx, limit)
	}
	runDiscoveryCrawlQueue = func(ctx context.Context) (*jobqueue.CrawlQueueSnapshot, error) {
		queue, queueAvailable := jobqueue.Default()
		controller, controllerAvailable := jobqueue.CrawlControl()
		if !queueAvailable || !controllerAvailable {
			return nil, errors.New("discovery crawl queue is unavailable")
		}
		if err := discovery.RecoverDueJobs(ctx, queue, time.Now()); err != nil {
			return nil, err
		}
		return controller.Snapshot(ctx, 50)
	}
	getAssessmentQueue = func(ctx context.Context, limit int) (*jobqueue.CrawlQueueSnapshot, error) {
		controller, available := jobqueue.AssessmentControl()
		if !available {
			return nil, errors.New("article assessment queue is unavailable")
		}
		return controller.Snapshot(ctx, limit)
	}
	runAssessmentQueue = func(ctx context.Context) (*jobqueue.CrawlQueueSnapshot, error) {
		queue, queueAvailable := jobqueue.Default()
		controller, controllerAvailable := jobqueue.AssessmentControl()
		if !queueAvailable || !controllerAvailable {
			return nil, errors.New("article assessment queue is unavailable")
		}
		if err := assessment.RecoverDueJobs(ctx, queue, time.Now()); err != nil {
			return nil, err
		}
		return controller.Run(ctx)
	}
	prepareArticleAssessmentBackfill = func(ctx context.Context, options assessment.ArticleAssessmentBatchOptions) (assessment.ArticleAssessmentBatchResult, error) {
		queue, _ := jobqueue.Default()
		return assessment.PrepareArticleAssessmentBackfill(ctx, assessment.ConfiguredArticleAssessor(), queue, options)
	}
	rollbackArticleAssessments = func(ctx context.Context, options assessment.ArticleAssessmentBatchOptions) (assessment.ArticleAssessmentBatchResult, error) {
		return assessment.RollbackArticleAssessment(ctx, assessment.ConfiguredArticleAssessor(), options)
	}
	startDiscoveryScheduler      = discovery.StartDiscoveryScheduler
	startRecommendationScheduler = recommendation.StartRecommendationScheduler
	startSharedJobQueue          = startApplicationJobQueue
	addDocFileToIndex            = search.AddDocFile
	deleteDocByHTMLPath          = search.DeleteDocByHTMLPath
	createBackupArchive          = backup.CreateBackup
	restoreBackupArchive         = backup.RestoreBackup
	initDatabase                 = bootstrap.InitDB
	createSearchIndex            = search.CreateDefaultIndex
	initArchiveQueue             = search.InitArchiveTaskQueue
	runGinRouter                 = func(router *gin.Engine, addr string) error {
		return router.Run(addr)
	}
)
