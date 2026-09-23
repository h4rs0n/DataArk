package discovery

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// RecoverDueJobs restores work that should not wait for the next scheduler
// interval after a process restart.
func RecoverDueJobs(ctx context.Context, queue DiscoveryJobEnqueuer, now time.Time) error {
	if db == nil || queue == nil {
		return nil
	}
	var recoveryErrors []error
	blacklist, err := loadDiscoveryDomainBlacklist()
	if err != nil {
		return err
	}

	var sources []DiscoverySource
	activeSiteIDs := db.Model(&DiscoverySite{}).Select("id").Where("crawl_allowed = ? AND status NOT IN ?", true, []string{DiscoverySiteStatusPaused, DiscoverySiteStatusBlocked, DiscoverySiteStatusNonBlog})
	if err := db.Where(`enabled = ? AND (
next_due_at <= ? OR
(next_due_at IS NULL AND next_fetch_at <= ?) OR
(next_due_at IS NULL AND next_fetch_at IS NULL)
)`, true, now, now).Where("site_id IS NULL OR site_id IN (?)", activeSiteIDs).Order("priority DESC, id").Find(&sources).Error; err != nil {
		return err
	}
	for _, source := range sources {
		deferred, reconcileErr := reconcileSourceFailureCooldown(&source, now)
		if reconcileErr != nil {
			recoveryErrors = append(recoveryErrors, reconcileErr)
			continue
		}
		if deferred {
			continue
		}
		if domainBlacklistMatchesURL(blacklist, source.URL) {
			continue
		}
		if err := queue.EnqueueFetchSource(ctx, source.ID); err != nil {
			recoveryErrors = append(recoveryErrors, err)
		}
	}

	var sites []DiscoverySite
	graphStatuses := []string{DiscoverySiteStatusSeed, DiscoverySiteStatusActive}
	if err := db.Where("status IN ? AND crawl_allowed = ? AND operational_pause = ? AND (next_graph_scan_at IS NULL OR next_graph_scan_at <= ?)", graphStatuses, true, "", now).Order("id").Find(&sites).Error; err != nil {
		recoveryErrors = append(recoveryErrors, err)
	} else {
		for _, site := range sites {
			if domainBlacklistMatchesURL(blacklist, site.RootURL) {
				continue
			}
			if err := queue.EnqueueScanBlogroll(ctx, site.ID); err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
	}

	var backfills []DiscoveryBackfillState
	if err := db.Where("status NOT IN ? AND (next_batch_at IS NULL OR next_batch_at <= ?) AND strategy <> ?", []string{"completed", "paused"}, now, BackfillStrategySitemap).Order("id").Find(&backfills).Error; err != nil {
		recoveryErrors = append(recoveryErrors, err)
	} else {
		for _, backfill := range backfills {
			var site DiscoverySite
			if err := db.Select("root_url").First(&site, backfill.SiteID).Error; err != nil {
				recoveryErrors = append(recoveryErrors, err)
				continue
			}
			if domainBlacklistMatchesURL(blacklist, site.RootURL) {
				continue
			}
			if err := queue.EnqueueBackfillSite(ctx, backfill.SiteID); err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
	}

	var candidates []DiscoveryCandidate
	processingStates := []string{"fetch_pending", "extract_pending", "dedupe_pending"}
	if err := Candidates(db).Where("(processing_state IN ? AND (next_processing_at IS NULL OR next_processing_at <= ?)) OR (processing_state = ? AND dedupe_state = ?)", processingStates, now, DiscoveryProcessingReady, DiscoveryDedupePending).Order("id").Find(&candidates).Error; err != nil {
		recoveryErrors = append(recoveryErrors, err)
	} else {
		for _, candidate := range candidates {
			if domainBlacklistMatchesURL(blacklist, candidate.URL) {
				continue
			}
			if err := queue.EnqueueProcessCandidate(ctx, candidate.ID, strconv.FormatUint(uint64(candidate.ContentVersion), 10)); err != nil {
				recoveryErrors = append(recoveryErrors, err)
			}
		}
	}

	return errors.Join(recoveryErrors...)
}

func reconcileSourceFailureCooldown(source *DiscoverySource, now time.Time) (bool, error) {
	if db == nil || source == nil || source.ID == 0 || source.FailureCount <= 0 || source.LastAttemptAt == nil {
		return false, nil
	}
	category := persistedSourceFailureCategory(*source)
	delay := ConfiguredSourceSchedulePolicy(discoveryClock).FailureDelay(source.ID, source.FailureCount, category)
	notBefore := source.LastAttemptAt.Add(delay)
	updates := map[string]interface{}{}
	if source.NextDueAt == nil || source.NextDueAt.Before(notBefore) {
		updates["next_due_at"] = &notBefore
		updates["next_fetch_at"] = &notBefore
		updates["backoff_until"] = &notBefore
		source.NextDueAt = &notBefore
		source.NextFetchAt = &notBefore
		source.BackoffUntil = &notBefore
	}
	if category != "" && category != source.BackoffReason {
		updates["backoff_reason"] = category
		source.BackoffReason = category
	}
	if len(updates) > 0 {
		updates["updated_at"] = now
		if err := db.Model(source).Updates(updates).Error; err != nil {
			return false, err
		}
	}
	return notBefore.After(now), nil
}
