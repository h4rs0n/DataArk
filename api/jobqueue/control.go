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
	DiscoveryQueueName = "discovery_crawl"
	CrawlQueueMode     = "manual"
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
	Mode      string           `json:"mode"`
	State     string           `json:"state"`
	CanRun    bool             `json:"canRun"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Counts    CrawlQueueCounts `json:"counts"`
	Tasks     []CrawlQueueTask `json:"tasks"`
}

type CrawlQueueController interface {
	Snapshot(context.Context, int) (*CrawlQueueSnapshot, error)
	Run(context.Context) (*CrawlQueueSnapshot, error)
}

func CrawlControl() (CrawlQueueController, bool) {
	defaultQueue.RLock()
	defer defaultQueue.RUnlock()
	return defaultQueue.entry.controller, defaultQueue.entry.controller != nil
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
	case ProcessCandidateJobKind:
		var args ProcessCandidateArgs
		if json.Unmarshal(encoded, &args) == nil {
			task.TargetType, task.TargetID, task.ContentVersion = "candidate", args.CandidateID, args.ContentVersion
		}
	}
}
