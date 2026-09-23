package jobqueue

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var (
	queueURLPattern    = regexp.MustCompile(`https?://[^\s]+`)
	queueSecretPattern = regexp.MustCompile(`(?i)(authorization|token|api[_-]?key|password)\s*[:=]\s*[^\s,;]+`)
)

const (
	DiscoveryQueueName  = "discovery_crawl"
	AssessmentQueueName = "article_assessment"
	CrawlQueueMode      = "automatic"
	AssessmentQueueMode = "manual"
)

type CrawlQueueCounts struct {
	Pending      int `json:"pending"`
	Running      int `json:"running"`
	Succeeded24h int `json:"succeeded24h"`
	Failed24h    int `json:"failed24h"`
}

type CrawlQueueTask struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	TargetType     string     `json:"targetType"`
	TargetID       uint       `json:"targetId"`
	ContentVersion string     `json:"contentVersion,omitempty"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	ScheduledAt    *time.Time `json:"scheduledAt,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type CrawlQueueSnapshot struct {
	Mode         string           `json:"mode"`
	State        string           `json:"state"`
	CanRun       bool             `json:"canRun"`
	UpdatedAt    time.Time        `json:"updatedAt"`
	RunStartedAt *time.Time       `json:"runStartedAt,omitempty"`
	Counts       CrawlQueueCounts `json:"counts"`
	Tasks        []CrawlQueueTask `json:"tasks"`
}

// CrawlQueueController 只提供观察快照：文章爬取由工人自动消费。
type CrawlQueueController interface {
	Snapshot(context.Context, int) (*CrawlQueueSnapshot, error)
}

// AssessmentQueueController 提供 LLM 评估队列快照，并允许 owner 手动启动一次消费。
type AssessmentQueueController interface {
	Snapshot(context.Context, int) (*CrawlQueueSnapshot, error)
	Run(context.Context) (*CrawlQueueSnapshot, error)
}

func CrawlControl() (CrawlQueueController, bool) {
	defaultQueue.RLock()
	defer defaultQueue.RUnlock()
	return defaultQueue.entry.crawl, defaultQueue.entry.crawl != nil
}

func AssessmentControl() (AssessmentQueueController, bool) {
	defaultQueue.RLock()
	defer defaultQueue.RUnlock()
	return defaultQueue.entry.assessment, defaultQueue.entry.assessment != nil
}

func normalizeSnapshotLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func isCrawlJobKind(kind string) bool {
	switch kind {
	case FetchSourceJobKind, ScanBlogrollJobKind, BackfillSiteJobKind, ProcessCandidateJobKind:
		return true
	default:
		return false
	}
}

func isAssessmentJobKind(kind string) bool {
	return kind == AssessArticleJobKind
}

func compactQueueError(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = queueURLPattern.ReplaceAllString(value, "[url]")
	value = queueSecretPattern.ReplaceAllString(value, "$1=[redacted]")
	runes := []rune(value)
	if len(runes) > 300 {
		return string(runes[:300])
	}
	return value
}

func decodeSafeCrawlTarget(task *CrawlQueueTask, encoded []byte) {
	if task == nil {
		return
	}
	switch task.Kind {
	case FetchSourceJobKind:
		var args FetchSourceArgs
		if json.Unmarshal(encoded, &args) == nil {
			task.TargetType, task.TargetID = "source", args.SourceID
		}
	case ScanBlogrollJobKind, BackfillSiteJobKind:
		var args struct {
			SiteID uint `json:"site_id"`
		}
		if json.Unmarshal(encoded, &args) == nil {
			task.TargetType, task.TargetID = "site", args.SiteID
		}
	case ProcessCandidateJobKind, AssessArticleJobKind:
		var args ProcessCandidateArgs
		if json.Unmarshal(encoded, &args) == nil && args.CandidateID != 0 {
			task.TargetType, task.TargetID, task.ContentVersion = "candidate", args.CandidateID, args.ContentVersion
			return
		}
		var assessArgs AssessArticleArgs
		if json.Unmarshal(encoded, &assessArgs) == nil {
			task.TargetType, task.TargetID, task.ContentVersion = "material", assessArgs.MaterialID, assessArgs.ContentVersion
		}
	}
}
