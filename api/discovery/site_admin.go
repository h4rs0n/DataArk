package discovery

import (
	"DataArk/jobqueue"
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

const (
	DiscoveryPauseOwnerPaused  = "owner_paused"
	DiscoveryPauseGlobalSafety = "global_safety_block"
)

var (
	ErrDiscoverySiteNotCrawlable    = errors.New("discovery site is paused or blocked")
	ErrDiscoveryJobQueueUnavailable = errors.New("discovery job queue is unavailable")
	ErrSitemapSourceDisabled        = errors.New("sitemap sources are disabled; use owner sitemap gap fill")
	ErrSitemapRequiresManualSeed    = errors.New("sitemap gap fill requires a manually managed seed")
	ErrSitemapDomainMismatch        = errors.New("sitemap URL must use the selected site's logical domain")
)

func UpdateDiscoverySiteOperationalStatus(siteID uint, status string, reason string) (*DiscoverySite, error) {
	if db == nil || siteID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	status = strings.TrimSpace(status)
	reason = strings.TrimSpace(reason)
	if !isOwnerManagedDiscoverySiteStatus(status) {
		return nil, errors.New("unsupported discovery site status")
	}
	clock := discoveryClock
	if clock == nil {
		clock = SystemClock{}
	}
	now := clock.Now()
	var site DiscoverySite
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&site, siteID).Error; err != nil {
			return err
		}
		updates := map[string]interface{}{"status": status, "updated_at": now}
		switch status {
		case DiscoverySiteStatusPaused:
			if reason == "" {
				reason = DiscoveryPauseOwnerPaused
			}
			updates["crawl_allowed"] = false
			updates["operational_pause"] = DiscoveryPauseOwnerPaused
			updates["operational_details"] = reason
			updates["next_graph_scan_at"] = nil
		case DiscoverySiteStatusBlocked:
			if reason == "" {
				reason = DiscoveryPauseGlobalSafety
			}
			updates["crawl_allowed"] = false
			updates["operational_pause"] = DiscoveryPauseGlobalSafety
			updates["operational_details"] = reason
			updates["next_graph_scan_at"] = nil
		default:
			updates["crawl_allowed"] = true
			updates["operational_pause"] = ""
			updates["operational_details"] = ""
			updates["next_graph_scan_at"] = &now
		}
		if err := tx.Model(&site).Updates(updates).Error; err != nil {
			return err
		}
		if status == DiscoverySiteStatusPaused || status == DiscoverySiteStatusBlocked {
			return tx.Model(&DiscoveryBackfillState{}).
				Where("site_id = ? AND status <> ?", siteID, BackfillStatusCompleted).
				Updates(map[string]interface{}{"status": BackfillStatusPaused, "completion_reason": updates["operational_pause"], "next_batch_at": nil, "updated_at": now}).Error
		}
		if err := tx.Model(&DiscoverySource{}).
			Where("site_id = ? AND endpoint_type = ?", siteID, DiscoveryEndpointHomepage).
			Updates(map[string]interface{}{"enabled": true, "next_due_at": &now, "next_fetch_at": &now}).Error; err != nil {
			return err
		}
		return tx.Model(&DiscoveryBackfillState{}).
			Where("site_id = ? AND status = ? AND completion_reason IN ?", siteID, BackfillStatusPaused, []string{DiscoveryPauseOwnerPaused, DiscoveryPauseGlobalSafety}).
			Updates(map[string]interface{}{"status": BackfillStatusPending, "completion_reason": "", "next_batch_at": &now, "updated_at": now}).Error
	})
	if err != nil {
		return nil, err
	}
	if err := db.First(&site, siteID).Error; err != nil {
		return nil, err
	}
	return &site, nil
}

func RequestDiscoverySiteBackfill(ctx context.Context, siteID uint) error {
	if db == nil || siteID == 0 {
		return gorm.ErrRecordNotFound
	}
	var site DiscoverySite
	if err := db.First(&site, siteID).Error; err != nil {
		return err
	}
	if !site.CrawlAllowed || site.Status == DiscoverySiteStatusPaused || site.Status == DiscoverySiteStatusBlocked || site.Status == DiscoverySiteStatusNonBlog {
		return ErrDiscoverySiteNotCrawlable
	}
	queue, available := jobqueue.Default()
	if !available {
		return ErrDiscoveryJobQueueUnavailable
	}
	return queue.EnqueueBackfillSite(ctx, siteID)
}

func RequestDiscoverySiteSitemapBackfill(ctx context.Context, siteID uint, rawURL string) error {
	queue, available := jobqueue.Default()
	if !available {
		return ErrDiscoveryJobQueueUnavailable
	}
	return requestDiscoverySiteSitemapBackfill(ctx, siteID, rawURL, queue)
}

func requestDiscoverySiteSitemapBackfill(ctx context.Context, siteID uint, rawURL string, queue JobEnqueuer) error {
	if db == nil || siteID == 0 {
		return gorm.ErrRecordNotFound
	}
	if queue == nil {
		return ErrDiscoveryJobQueueUnavailable
	}
	normalizedURL, err := NormalizeDiscoveryURL(rawURL)
	if err != nil {
		return err
	}
	clock := discoveryClock
	if clock == nil {
		clock = SystemClock{}
	}
	now := clock.Now()
	err = db.Transaction(func(tx *gorm.DB) error {
		var site DiscoverySite
		if err := tx.First(&site, siteID).Error; err != nil {
			return err
		}
		if !site.CrawlAllowed || site.Status != DiscoverySiteStatusSeed {
			return ErrSitemapRequiresManualSeed
		}
		sitemapDomain, err := domainKeyForURL(normalizedURL)
		if err != nil {
			return err
		}
		if sitemapDomain != siteDomainKey(site) {
			return ErrSitemapDomainMismatch
		}
		var managed int64
		if err := tx.Model(&DiscoverySource{}).Where("site_id = ? AND user_managed = ?", site.ID, true).Count(&managed).Error; err != nil {
			return err
		}
		if managed == 0 {
			return ErrSitemapRequiresManualSeed
		}
		cursor := encodeBackfillCursor(backfillCursor{Pending: []string{normalizedURL}, Visited: []string{}})
		var state DiscoveryBackfillState
		findErr := tx.Where("site_id = ? AND strategy = ?", site.ID, BackfillStrategySitemap).First(&state).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			state = DiscoveryBackfillState{
				SiteID: site.ID, Strategy: BackfillStrategySitemap, Cursor: cursor,
				Status: BackfillStatusPending, NextBatchAt: &now, OwnerRequestedAt: &now,
				CreatedAt: now, UpdatedAt: now,
			}
			return tx.Create(&state).Error
		}
		if findErr != nil {
			return findErr
		}
		return tx.Model(&state).Updates(map[string]interface{}{
			"cursor": cursor, "status": BackfillStatusPending, "completion_reason": "",
			"failure_count": 0, "next_batch_at": &now, "owner_requested_at": &now, "updated_at": now,
		}).Error
	})
	if err != nil {
		return err
	}
	return queue.EnqueueBackfillSite(ctx, siteID)
}

func discoverySourceOperationallyCrawlable(source *DiscoverySource) bool {
	if db == nil || source == nil || source.SiteID == nil {
		return true
	}
	var site DiscoverySite
	if err := db.Select("status", "crawl_allowed").First(&site, *source.SiteID).Error; err != nil {
		return false
	}
	return site.CrawlAllowed && site.Status != DiscoverySiteStatusPaused && site.Status != DiscoverySiteStatusBlocked && site.Status != DiscoverySiteStatusNonBlog
}

func isOwnerManagedDiscoverySiteStatus(status string) bool {
	switch status {
	case DiscoverySiteStatusSeed, DiscoverySiteStatusObserving, DiscoverySiteStatusActive, DiscoverySiteStatusPaused, DiscoverySiteStatusBlocked:
		return true
	default:
		return false
	}
}
