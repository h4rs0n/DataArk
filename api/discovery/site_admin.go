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
