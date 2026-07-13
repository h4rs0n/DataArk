package discovery

import (
	"DataArk/jobqueue"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const maxDedicatedBlogrollPages = 10

type BlogrollScanResult struct {
	SiteID          uint
	PagesScanned    int
	CandidatesFound int
	Graph           GraphApplyResult
}

func RunScanBlogrollJob(ctx context.Context, siteID uint) error {
	queue, _ := jobqueue.Default()
	_, err := ScanBlogroll(ctx, siteID, queue)
	return err
}

func ScanBlogroll(ctx context.Context, siteID uint, queue JobEnqueuer) (BlogrollScanResult, error) {
	result := BlogrollScanResult{SiteID: siteID}
	if db == nil {
		return result, nil
	}
	var site DiscoverySite
	if err := db.First(&site, siteID).Error; err != nil {
		return result, err
	}
	if !site.CrawlAllowed || site.Status == DiscoverySiteStatusPaused || site.Status == DiscoverySiteStatusBlocked || site.Status == DiscoverySiteStatusNonBlog {
		return result, nil
	}
	clock := discoveryClock
	if clock == nil {
		clock = SystemClock{}
	}
	now := clock.Now()
	discoverer := BlogrollDiscoverer{}
	root, err := fetchDiscoveryRequest(ctx, FetchRequest{URL: site.RootURL, Kind: FetchKindHTML})
	if err != nil {
		return result, finishGraphScan(&site, now, nil, err)
	}
	if root.StatusCode < http.StatusOK || root.StatusCode >= http.StatusMultipleChoices {
		err = fmt.Errorf("%w: %d", ErrHTTPFetchStatus, root.StatusCode)
		return result, finishGraphScan(&site, now, &root, err)
	}
	rootDiscovery, err := discoverer.Discover(root.Body, root.FinalURLOr(site.RootURL), site.RootURL)
	if err != nil {
		return result, finishGraphScan(&site, now, &root, err)
	}
	result.PagesScanned = 1
	links := append([]BlogrollLink(nil), rootDiscovery.Links...)
	for index, pageURL := range rootDiscovery.DedicatedURLs {
		if index >= maxDedicatedBlogrollPages {
			break
		}
		page, fetchErr := fetchDiscoveryRequest(ctx, FetchRequest{URL: pageURL, Kind: FetchKindHTML})
		if fetchErr != nil || page.StatusCode < http.StatusOK || page.StatusCode >= http.StatusMultipleChoices {
			continue
		}
		pageDiscovery, parseErr := discoverer.Discover(page.Body, page.FinalURLOr(pageURL), site.RootURL)
		if parseErr != nil {
			continue
		}
		result.PagesScanned++
		links = append(links, pageDiscovery.Links...)
	}
	links = dedupeBlogrollLinks(links)
	result.CandidatesFound = len(links)
	graphService := SiteGraphService{Clock: clock, Queue: queue, Classifier: SiteClassifier{}}
	result.Graph, err = graphService.ApplyLinks(ctx, site, links)
	if err != nil {
		return result, finishGraphScan(&site, now, &root, err)
	}
	if err := finishGraphScan(&site, now, &root, nil); err != nil {
		return result, err
	}
	if result.Graph.TargetsPending > 0 {
		next := now.Add(failedGraphRescanInterval)
		if err := db.Model(&site).Update("next_graph_scan_at", &next).Error; err != nil {
			return result, err
		}
	}
	return result, nil
}

func (result FetchResult) FinalURLOr(fallback string) string {
	if strings.TrimSpace(result.FinalURL) != "" {
		return result.FinalURL
	}
	return fallback
}

func finishGraphScan(site *DiscoverySite, now time.Time, fetched *FetchResult, scanErr error) error {
	if db == nil || site == nil {
		return scanErr
	}
	updates := map[string]interface{}{"last_validated_at": &now}
	if fetched != nil && fetched.RobotsStatus != "" {
		updates["robots_status"] = fetched.RobotsStatus
	}
	if scanErr != nil {
		next := now.Add(failedGraphRescanInterval)
		updates["next_graph_scan_at"] = &next
		updates["operational_details"] = truncateOperationalError(scanErr.Error())
		if errors.Is(scanErr, ErrRobotsDisallowed) {
			updates["robots_status"] = "disallowed"
			updates["crawl_allowed"] = false
			updates["operational_pause"] = DiscoveryPauseRobots
			updates["next_graph_scan_at"] = nil
		} else if errors.Is(scanErr, ErrRobotsUnavailable) {
			updates["robots_status"] = "unavailable"
		}
		if err := db.Model(site).Updates(updates).Error; err != nil {
			return err
		}
		return scanErr
	}
	next := now.Add(defaultGraphRescanInterval)
	updates["next_graph_scan_at"] = &next
	updates["operational_details"] = ""
	if err := db.Model(site).Updates(updates).Error; err != nil {
		return err
	}
	return nil
}

func truncateOperationalError(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 512 {
		return value[:512]
	}
	return value
}
