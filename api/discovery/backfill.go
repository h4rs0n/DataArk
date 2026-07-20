package discovery

import (
	"DataArk/config"
	"DataArk/jobqueue"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gorm.io/gorm"
)

const (
	BackfillStrategySitemap          = "sitemap"
	BackfillStrategyArchive          = "archive"
	BackfillStatusPending            = "pending"
	BackfillStatusRunning            = "running"
	BackfillStatusCompleted          = "completed"
	BackfillStatusPaused             = "paused"
	BackfillPauseSitemapOwnerRequest = "sitemap_requires_owner_request"
	DiscoveryMethodArchive           = "archive"
)

type backfillCursor struct {
	Pending []string `json:"pending"`
	Visited []string `json:"visited"`
}

type BackfillCoverage struct {
	State               DiscoveryBackfillState `json:"state"`
	EstimatedCompletion float64                `json:"estimatedCompletion"`
	PendingURLs         int                    `json:"pendingUrls"`
	VisitedURLs         int                    `json:"visitedUrls"`
}

func RunBackfillSiteJob(ctx context.Context, siteID uint) error {
	queue, _ := jobqueue.Default()
	_, err := RunBackfillSite(ctx, siteID, queue)
	return err
}

func RunBackfillSite(ctx context.Context, siteID uint, queue JobEnqueuer) (*DiscoveryBackfillState, error) {
	if db == nil {
		return nil, nil
	}
	clock := discoveryClock
	if clock == nil {
		clock = SystemClock{}
	}
	now := clock.Now()
	var site DiscoverySite
	if err := db.First(&site, siteID).Error; err != nil {
		return nil, err
	}
	if err := ensureDiscoveryURLNotBlacklisted(ctx, site.RootURL); err != nil {
		return nil, err
	}
	if !site.CrawlAllowed || site.Status == DiscoverySiteStatusPaused || site.Status == DiscoverySiteStatusBlocked || site.Status == DiscoverySiteStatusNonBlog {
		reason := firstNonBlank(site.OperationalPause, "site_not_crawlable")
		if err := db.Model(&DiscoveryBackfillState{}).Where("site_id = ? AND status <> ?", siteID, BackfillStatusCompleted).Updates(map[string]interface{}{"status": BackfillStatusPaused, "completion_reason": reason, "next_batch_at": nil}).Error; err != nil {
			return nil, err
		}
		return nil, nil
	}
	var states []DiscoveryBackfillState
	if err := db.Where("site_id = ? AND status NOT IN ? AND (next_batch_at IS NULL OR next_batch_at <= ?) AND (strategy <> ? OR owner_requested_at IS NOT NULL)", siteID, []string{BackfillStatusCompleted, BackfillStatusPaused}, now, BackfillStrategySitemap).Find(&states).Error; err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, nil
	}
	sort.SliceStable(states, func(i, j int) bool {
		return backfillStrategyPriority(states[i].Strategy) < backfillStrategyPriority(states[j].Strategy)
	})
	state := states[0]
	cursor := decodeBackfillCursor(state.Cursor)
	if len(cursor.Pending) == 0 {
		state.Status = BackfillStatusCompleted
		state.CompletionReason = "cursor_exhausted"
		state.NextBatchAt = nil
		if err := db.Model(&state).Updates(map[string]interface{}{"status": state.Status, "completion_reason": state.CompletionReason, "next_batch_at": nil, "last_batch_at": &now}).Error; err != nil {
			return &state, err
		}
		return &state, nil
	}
	batchSize := positiveOr(config.DISCOVERYBACKFILLBATCHSIZE, 1)
	if batchSize > 20 {
		batchSize = 20
	}
	processed := 0
	createdCount := 0
	duplicateCount := 0
	urlsSeen := 0
	var earliest, latest *time.Time
	for processed < batchSize && len(cursor.Pending) > 0 {
		currentURL := cursor.Pending[0]
		response, fetchErr := fetchBackfillPage(ctx, state.Strategy, currentURL)
		if fetchErr != nil {
			return &state, recordBackfillFailure(&state, cursor, now, fetchErr)
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			fetchErr = fmt.Errorf("%w: %d", ErrHTTPFetchStatus, response.StatusCode)
			return &state, recordBackfillFailure(&state, cursor, now, fetchErr)
		}
		urlsSeen++
		var candidates []discoveredCandidate
		var nextURLs []string
		switch state.Strategy {
		case BackfillStrategySitemap:
			candidates, nextURLs, fetchErr = parseSitemapDocument(response.Body, site.RootURL)
		case BackfillStrategyArchive:
			candidates, nextURLs, fetchErr = parseHistoricalPage(response.Body, response.FinalURLOr(currentURL), site.RootURL, now)
		default:
			fetchErr = fmt.Errorf("unsupported backfill strategy %q", state.Strategy)
		}
		if fetchErr != nil {
			return &state, recordBackfillFailure(&state, cursor, now, fetchErr)
		}
		source, sourceErr := sourceForBackfill(site, state.Strategy, currentURL)
		if sourceErr != nil {
			return &state, recordBackfillFailure(&state, cursor, now, sourceErr)
		}
		for index := range candidates {
			candidates[index].SourcePageURL = currentURL
			if candidates[index].PublishedAt == nil {
				discoveredAt := now
				candidates[index].PublishedAt = &discoveredAt
				candidates[index].PublishedConfidence = "discovered_at"
			}
			writeResult, writeErr := upsertDiscoveryCandidate(source, candidates[index])
			if writeErr != nil {
				return &state, recordBackfillFailure(&state, cursor, now, writeErr)
			}
			urlsSeen++
			if writeResult.Created {
				createdCount++
				if queue != nil && writeResult.Candidate.ProcessingState != DiscoveryProcessingDomainBlocked {
					_ = queue.EnqueueProcessCandidate(ctx, writeResult.Candidate.ID, candidateContentVersion(writeResult.Candidate))
				}
			} else {
				duplicateCount++
			}
			earliest, latest = expandCoverage(earliest, latest, candidates[index].PublishedAt)
		}
		cursor.Pending = cursor.Pending[1:]
		cursor.Visited = appendUniqueURL(cursor.Visited, currentURL)
		for _, nextURL := range nextURLs {
			if !containsURL(cursor.Visited, nextURL) {
				cursor.Pending = appendUniqueURL(cursor.Pending, nextURL)
			}
		}
		processed++
	}
	state.BatchNumber++
	state.URLsSeen += uint(urlsSeen)
	state.ArticlesFound += uint(createdCount)
	state.DuplicateCount += uint(duplicateCount)
	state.FailureCount = 0
	state.LastSuccessAt = &now
	state.LastBatchAt = &now
	state.Cursor = encodeBackfillCursor(cursor)
	state.Status = BackfillStatusPending
	state.CompletionReason = ""
	updates := map[string]interface{}{
		"cursor": state.Cursor, "batch_number": state.BatchNumber, "urls_seen": state.URLsSeen,
		"articles_found": state.ArticlesFound, "duplicate_count": state.DuplicateCount,
		"failure_count": 0, "last_success_at": &now, "last_batch_at": &now,
		"status": state.Status, "completion_reason": "",
	}
	if earliest != nil && (state.EarliestCovered == nil || earliest.Before(*state.EarliestCovered)) {
		state.EarliestCovered = earliest
		updates["earliest_covered"] = earliest
	}
	if latest != nil && (state.LatestCovered == nil || latest.After(*state.LatestCovered)) {
		state.LatestCovered = latest
		updates["latest_covered"] = latest
	}
	if len(cursor.Pending) == 0 {
		state.Status = BackfillStatusCompleted
		state.CompletionReason = "cursor_exhausted"
		state.NextBatchAt = nil
		updates["status"] = state.Status
		updates["completion_reason"] = state.CompletionReason
		updates["next_batch_at"] = nil
	} else {
		interval := configuredDuration(config.DISCOVERYBACKFILLMAXINTERVAL, 7*24*time.Hour)
		if createdCount > 0 {
			interval = time.Hour
		}
		next := now.Add(interval)
		state.NextBatchAt = &next
		updates["next_batch_at"] = &next
	}
	if err := db.Model(&state).Updates(updates).Error; err != nil {
		return &state, err
	}
	return &state, nil
}

func ensureBackfillState(siteID uint, strategy string, urls []string, now time.Time) (DiscoveryBackfillState, bool, error) {
	state := DiscoveryBackfillState{}
	scheduled := false
	if db == nil || len(urls) == 0 {
		return state, false, nil
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		findErr := tx.Where("site_id = ? AND strategy = ?", siteID, strategy).First(&state).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			cursor := backfillCursor{Pending: uniqueNormalizedURLs(urls), Visited: []string{}}
			state = DiscoveryBackfillState{SiteID: siteID, Strategy: strategy, Cursor: encodeBackfillCursor(cursor), Status: BackfillStatusPending, NextBatchAt: &now, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&state).Error; err != nil {
				return err
			}
			scheduled = true
			return nil
		}
		if findErr != nil {
			return findErr
		}
		cursor := decodeBackfillCursor(state.Cursor)
		before := len(cursor.Pending)
		for _, rawURL := range uniqueNormalizedURLs(urls) {
			if !containsURL(cursor.Visited, rawURL) {
				cursor.Pending = appendUniqueURL(cursor.Pending, rawURL)
			}
		}
		updates := map[string]interface{}{"cursor": encodeBackfillCursor(cursor)}
		if len(cursor.Pending) > before {
			scheduled = true
		}
		if state.Status == BackfillStatusCompleted && len(cursor.Pending) > 0 {
			updates["status"] = BackfillStatusPending
			updates["completion_reason"] = ""
			updates["next_batch_at"] = &now
		}
		return tx.Model(&state).Updates(updates).Error
	})
	return state, scheduled, err
}

func fetchBackfillPage(ctx context.Context, strategy string, rawURL string) (FetchResult, error) {
	kind := FetchKindHTML
	if strategy == BackfillStrategySitemap {
		kind = FetchKindSitemap
	}
	return fetchDiscoveryRequest(ctx, FetchRequest{URL: rawURL, Kind: kind})
}

func parseHistoricalPage(body []byte, pageURL string, siteRootURL string, now time.Time) ([]discoveredCandidate, []string, error) {
	document, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, err
	}
	pageBase, err := neturl.Parse(pageURL)
	if err != nil {
		return nil, nil, err
	}
	root, err := neturl.Parse(siteRootURL)
	if err != nil {
		return nil, nil, err
	}
	candidateMap := make(map[string]discoveredCandidate)
	nextSet := make(map[string]struct{})
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			if resolved, ok := resolveWebURL(attrValue(node, "href"), pageBase); ok && sameLogicalHost(resolved, root) {
				if isHistoricalNavigation(resolved.Path, compactNodeText(node, 120)) {
					nextSet[resolved.String()] = struct{}{}
				} else if articleURL, ok := sameHostArticleURL(resolved.String(), root); ok && !isNonArticleNavigation(articleURL, compactNodeText(node, 120)) {
					published := now
					candidateMap[articleURL] = discoveredCandidate{URL: articleURL, Title: compactNodeText(node, 160), PublishedAt: &published, PublishedConfidence: "discovered_at", DiscoveryMethod: DiscoveryMethodArchive, MetadataConfidence: 30}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	candidates := make([]discoveredCandidate, 0, len(candidateMap))
	for _, candidate := range candidateMap {
		candidates = append(candidates, candidate)
	}
	return candidates, sortedKeys(nextSet), nil
}

func isHistoricalNavigation(path string, anchor string) bool {
	lowerPath := strings.ToLower(path)
	lowerAnchor := strings.ToLower(strings.TrimSpace(anchor))
	for _, marker := range []string{"/archive/", "/archives/", "/page/", "page-"} {
		if strings.Contains(lowerPath, marker) {
			return true
		}
	}
	for _, marker := range []string{"archive", "archives", "older posts", "older entries", "next page"} {
		if lowerAnchor == marker || strings.Contains(lowerAnchor, marker) {
			return true
		}
	}
	for _, segment := range strings.Split(strings.Trim(path, "/"), "/") {
		if year, err := strconv.Atoi(segment); err == nil && year >= 1990 && year <= 2100 {
			return true
		}
	}
	return false
}

func sourceForBackfill(site DiscoverySite, strategy string, currentURL string) (DiscoverySource, error) {
	var source DiscoverySource
	if strategy == BackfillStrategySitemap {
		return source, db.Where("site_id = ? AND endpoint_type = ?", site.ID, DiscoveryEndpointHomepage).First(&source).Error
	}
	return source, db.Where("site_id = ? AND endpoint_type = ?", site.ID, DiscoveryEndpointHomepage).First(&source).Error
}

func recordBackfillFailure(state *DiscoveryBackfillState, cursor backfillCursor, now time.Time, failure error) error {
	state.FailureCount++
	state.LastBatchAt = &now
	state.Cursor = encodeBackfillCursor(cursor)
	updates := map[string]interface{}{"failure_count": state.FailureCount, "last_batch_at": &now, "cursor": state.Cursor}
	if errors.Is(failure, ErrRobotsDisallowed) || (backfillFailureIsUnrecoverable(failure) && state.FailureCount >= 5) {
		state.Status = BackfillStatusPaused
		state.CompletionReason = "consecutive_unrecoverable_errors"
		if errors.Is(failure, ErrRobotsDisallowed) {
			state.CompletionReason = DiscoveryPauseRobots
		}
		state.NextBatchAt = nil
		updates["status"] = state.Status
		updates["completion_reason"] = state.CompletionReason
		updates["next_batch_at"] = nil
	} else {
		next := ConfiguredSourceSchedulePolicy(discoveryClock).NextFailure(state.ID, int(state.FailureCount))
		state.NextBatchAt = &next
		updates["status"] = BackfillStatusPending
		updates["next_batch_at"] = &next
	}
	if err := db.Model(state).Updates(updates).Error; err != nil {
		return err
	}
	return failure
}

func backfillFailureIsUnrecoverable(err error) bool {
	return errors.Is(err, ErrDiscoveryDomainBlacklisted) || errors.Is(err, ErrRobotsDisallowed) || errors.Is(err, ErrUnsafeURLScheme) || errors.Is(err, ErrUnsafeURLHost) || errors.Is(err, ErrUnsafeURLPort) || errors.Is(err, ErrUnsafeIPAddress) || errors.Is(err, ErrHTTPFetchContentType)
}

func ListBackfillCoverage(siteID uint) ([]BackfillCoverage, error) {
	states := make([]DiscoveryBackfillState, 0)
	if db == nil {
		return []BackfillCoverage{}, nil
	}
	if err := db.Where("site_id = ?", siteID).Order("strategy").Find(&states).Error; err != nil {
		return nil, err
	}
	result := make([]BackfillCoverage, 0, len(states))
	for _, state := range states {
		cursor := decodeBackfillCursor(state.Cursor)
		total := len(cursor.Pending) + len(cursor.Visited)
		estimate := 0.0
		if total > 0 {
			estimate = float64(len(cursor.Visited)) / float64(total)
		} else if state.Status == BackfillStatusCompleted {
			estimate = 1
		}
		result = append(result, BackfillCoverage{State: state, EstimatedCompletion: estimate, PendingURLs: len(cursor.Pending), VisitedURLs: len(cursor.Visited)})
	}
	return result, nil
}

func decodeBackfillCursor(value string) backfillCursor {
	cursor := backfillCursor{Pending: []string{}, Visited: []string{}}
	_ = json.Unmarshal([]byte(value), &cursor)
	return cursor
}

func encodeBackfillCursor(cursor backfillCursor) string {
	body, _ := json.Marshal(cursor)
	return string(body)
}

func appendUniqueURL(values []string, value string) []string {
	if containsURL(values, value) {
		return values
	}
	return append(values, value)
}

func containsURL(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func expandCoverage(earliest *time.Time, latest *time.Time, value *time.Time) (*time.Time, *time.Time) {
	if value == nil {
		return earliest, latest
	}
	if earliest == nil || value.Before(*earliest) {
		copy := *value
		earliest = &copy
	}
	if latest == nil || value.After(*latest) {
		copy := *value
		latest = &copy
	}
	return earliest, latest
}

func backfillStrategyPriority(strategy string) int {
	switch strategy {
	case BackfillStrategySitemap:
		return 0
	case BackfillStrategyArchive:
		return 1
	default:
		return 2
	}
}
