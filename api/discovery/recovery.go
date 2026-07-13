package discovery

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// RecoverDueJobs restores work that should not wait for the next scheduler
// interval after a process restart.
func RecoverDueJobs(ctx context.Context, queue JobEnqueuer, now time.Time) error {
	if db == nil || queue == nil {
		return nil
	}
	var recoveryErrors []error

	var sources []DiscoverySource
	if err := db.Where(`enabled = ? AND (
next_due_at <= ? OR
(next_due_at IS NULL AND next_fetch_at <= ?) OR
(next_due_at IS NULL AND next_fetch_at IS NULL)
)`, true, now, now).Order("id").Find(&sources).Error; err != nil {
		return err
	}
	for _, source := range sources {
		if err := queue.EnqueueFetchSource(ctx, source.ID); err != nil {
			recoveryErrors = append(recoveryErrors, err)
		}
	}

	var sites []DiscoverySite
	graphStatuses := []string{DiscoverySiteStatusSeed, DiscoverySiteStatusObserving, DiscoverySiteStatusActive}
	if err := db.Where("status IN ? AND crawl_allowed = ? AND operational_pause = ? AND (next_graph_scan_at IS NULL OR next_graph_scan_at <= ?)", graphStatuses, true, "", now).Order("id").Find(&sites).Error; err != nil {
		recoveryErrors = append(recoveryErrors, err)
	} else {
		for _, site := range sites {
			if err := queue.EnqueueScanBlogroll(ctx, site.ID); err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
	}

	var backfills []DiscoveryBackfillState
	if err := db.Where("status NOT IN ? AND (next_batch_at IS NULL OR next_batch_at <= ?)", []string{"completed", "paused"}, now).Order("id").Find(&backfills).Error; err != nil {
		recoveryErrors = append(recoveryErrors, err)
	} else {
		for _, backfill := range backfills {
			if err := queue.EnqueueBackfillSite(ctx, backfill.SiteID); err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
	}

	var candidates []DiscoveryCandidate
	processingStates := []string{"fetch_pending", "extract_pending", "dedupe_pending", "assessment_pending"}
	if err := db.Where("processing_state IN ?", processingStates).Order("id").Find(&candidates).Error; err != nil {
		recoveryErrors = append(recoveryErrors, err)
	} else {
		for _, candidate := range candidates {
			if err := queue.EnqueueProcessCandidate(ctx, candidate.ID, strconv.FormatUint(uint64(candidate.ContentVersion), 10)); err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
	}
	return errors.Join(recoveryErrors...)
}
