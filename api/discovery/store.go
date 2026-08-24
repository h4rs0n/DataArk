package discovery

import (
	"DataArk/archive"
	"DataArk/config"
	"DataArk/discovery/articlerules"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"math"
	neturl "net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

const (
	DiscoverySourceTypeFeed   = "feed"
	DiscoverySourceTypeRSSHub = "rsshub"
	DiscoverySourceTypeSite   = "site"

	DiscoveryCandidateStatusNew      = "new"
	DiscoveryCandidateStatusRead     = "read"
	DiscoveryCandidateStatusIgnored  = "ignored"
	DiscoveryCandidateStatusArchived = "archived"

	DiscoveryCandidateEnrichmentStatusPending = "pending"
)

var ErrFeedParse = errors.New("feed payload could not be parsed")

var fetchDiscoveryRequest = func(ctx context.Context, request FetchRequest) (FetchResult, error) {
	return ConfiguredHTTPFetcher().Fetch(ctx, request)
}

type DiscoveryFetchResult struct {
	SourceID    uint   `json:"sourceId"`
	Discovered  int    `json:"discovered"`
	Stored      int    `json:"stored"`
	FeedsFound  int    `json:"feedsFound"`
	LinksFound  int    `json:"linksFound"`
	SourceError string `json:"sourceError"`
}

type discoveredCandidate struct {
	URL                 string
	Title               string
	Summary             string
	PublishedAt         *time.Time
	PublishedConfidence string
	DiscoveryMethod     string
	SourcePageURL       string
	MetadataConfidence  int
}

func ListDiscoverySources() ([]DiscoverySource, error) {
	sources := make([]DiscoverySource, 0)
	if db == nil {
		return sources, nil
	}
	if err := db.Where("user_managed = ?", true).Order("id").Find(&sources).Error; err != nil {
		return nil, err
	}
	var sites []DiscoverySite
	if err := db.Order("id").Find(&sites).Error; err != nil {
		return nil, err
	}
	siteByID := make(map[uint]DiscoverySite, len(sites))
	for _, site := range sites {
		siteByID[site.ID] = site
	}
	grouped := make([]DiscoverySource, 0, len(sources))
	seenDomains := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		domainKey := ""
		if source.SiteID != nil {
			site := siteByID[*source.SiteID]
			if site.Status == DiscoverySiteStatusNonBlog {
				continue
			}
			domainKey = siteDomainKey(site)
		}
		if domainKey == "" {
			domainKey, _ = domainKeyForURL(source.URL)
		}
		if domainKey == "" {
			domainKey = fmt.Sprintf("source:%d", source.ID)
		}
		if _, exists := seenDomains[domainKey]; exists {
			continue
		}
		seenDomains[domainKey] = struct{}{}
		grouped = append(grouped, source)
	}
	sort.SliceStable(grouped, func(i, j int) bool {
		return grouped[i].CreatedAt.After(grouped[j].CreatedAt)
	})
	return grouped, nil
}

func CreateDiscoverySource(name string, rawURL string, sourceType string, enabled bool) (*DiscoverySource, error) {
	normalizedURL, err := NormalizeDiscoveryURL(rawURL)
	if err != nil {
		return nil, err
	}
	if err := ensureDiscoveryURLNotBlacklisted(context.Background(), normalizedURL); err != nil {
		return nil, err
	}
	sourceType = normalizeDiscoverySourceType(sourceType)
	if sourceType == DiscoverySourceTypeSitemap {
		return nil, ErrSitemapSourceDisabled
	}
	if strings.TrimSpace(name) == "" {
		name = hostLabel(normalizedURL)
	}
	source := &DiscoverySource{
		Name:         strings.TrimSpace(name),
		URL:          normalizedURL,
		CrawlHost:    crawlHostForURL(normalizedURL),
		Type:         sourceType,
		EndpointType: legacyEndpointType(sourceType),
		UserManaged:  true,
		Priority:     DiscoveryPriorityManual,
		Enabled:      enabled,
	}
	if enabled {
		now := discoveryClock.Now()
		source.NextDueAt = &now
		source.NextFetchAt = &now
	}
	if db == nil {
		return source, nil
	}
	now := discoveryClock.Now()
	var site DiscoverySite
	var homepage DiscoverySource
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		site, err = ensureManualSeedSite(tx, source, now)
		if err != nil {
			return err
		}
		source.SiteID = &site.ID
		if err := tx.Create(source).Error; err != nil {
			return err
		}
		homepage, err = ensureHomepageEndpoint(tx, site, now)
		return err
	}); err != nil {
		return nil, err
	}
	if queue, available := jobQueue(); available {
		if err := queue.EnqueueFetchSource(context.Background(), source.ID); err != nil {
			log.Printf("new discovery seed source %d fetch enqueue failed: %v", source.ID, err)
		}
		if homepage.ID != source.ID {
			if err := queue.EnqueueFetchSource(context.Background(), homepage.ID); err != nil {
				log.Printf("new discovery seed homepage %d fetch enqueue failed: %v", homepage.ID, err)
			}
		}
		if err := queue.EnqueueScanBlogroll(context.Background(), site.ID); err != nil {
			log.Printf("new discovery seed site %d blogroll enqueue failed: %v", site.ID, err)
		}
	}
	return source, nil
}

func UpdateDiscoverySource(id uint, name string, rawURL string, sourceType string, enabled bool) (*DiscoverySource, error) {
	if db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var source DiscoverySource
	if err := db.Where("user_managed = ?", true).First(&source, id).Error; err != nil {
		return nil, err
	}
	normalizedURL, err := NormalizeDiscoveryURL(rawURL)
	if err != nil {
		return nil, err
	}
	if err := ensureDiscoveryURLNotBlacklisted(context.Background(), normalizedURL); err != nil {
		return nil, err
	}
	sourceType = normalizeDiscoverySourceType(sourceType)
	if sourceType == DiscoverySourceTypeSitemap {
		return nil, ErrSitemapSourceDisabled
	}
	updates := map[string]interface{}{
		"name":          strings.TrimSpace(name),
		"url":           normalizedURL,
		"crawl_host":    crawlHostForURL(normalizedURL),
		"type":          sourceType,
		"endpoint_type": legacyEndpointType(sourceType),
		"enabled":       enabled,
	}
	if strings.TrimSpace(name) == "" {
		updates["name"] = hostLabel(normalizedURL)
	}
	if err := db.Model(&source).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

func DeleteDiscoverySource(id uint) error {
	if db == nil {
		return nil
	}
	return db.Where("user_managed = ?", true).Delete(&DiscoverySource{}, id).Error
}

func GetDiscoveryCandidate(id uint) (*DiscoveryCandidate, error) {
	if db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, id).Error; err != nil {
		return nil, err
	}
	return &candidate, nil
}

func FetchDiscoverySourceByID(ctx context.Context, id uint) (*DiscoveryFetchResult, error) {
	if db == nil {
		return &DiscoveryFetchResult{SourceID: id}, nil
	}
	var source DiscoverySource
	if err := db.First(&source, id).Error; err != nil {
		return nil, err
	}
	return FetchDiscoverySource(ctx, &source)
}

// RunFetchDiscoverySourceJob discards stale queued work without issuing a
// network request. Direct owner refreshes continue to use
// FetchDiscoverySourceByID and intentionally bypass the schedule check.
func RunFetchDiscoverySourceJob(ctx context.Context, id uint) error {
	if db == nil {
		return nil
	}
	var source DiscoverySource
	if err := db.First(&source, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if !source.Enabled {
		return nil
	}
	if !discoverySourceOperationallyCrawlable(&source) {
		return nil
	}
	now := discoveryClock.Now()
	deferred, err := reconcileSourceFailureCooldown(&source, now)
	if err != nil {
		return err
	}
	nextDueAt := source.NextDueAt
	if nextDueAt == nil {
		nextDueAt = source.NextFetchAt
	}
	if deferred || (nextDueAt != nil && nextDueAt.After(now)) {
		return nil
	}
	_, err = FetchDiscoverySource(ctx, &source)
	return err
}

func FetchDiscoverySource(ctx context.Context, source *DiscoverySource) (*DiscoveryFetchResult, error) {
	if source == nil {
		return nil, errors.New("missing discovery source")
	}
	if err := ensureDiscoveryURLNotBlacklisted(ctx, source.URL); err != nil {
		return &DiscoveryFetchResult{SourceID: source.ID, SourceError: err.Error()}, err
	}
	if source.EndpointType == DiscoveryEndpointSitemap || normalizeDiscoverySourceType(source.Type) == DiscoverySourceTypeSitemap {
		return &DiscoveryFetchResult{SourceID: source.ID, SourceError: ErrSitemapSourceDisabled.Error()}, ErrSitemapSourceDisabled
	}
	if !discoverySourceOperationallyCrawlable(source) {
		return &DiscoveryFetchResult{SourceID: source.ID, SourceError: ErrDiscoverySiteNotCrawlable.Error()}, ErrDiscoverySiteNotCrawlable
	}
	result := &DiscoveryFetchResult{SourceID: source.ID}
	startedAt := discoveryClock.Now()
	candidates, feedsFound, linksFound, fetchResult, err := discoverCandidates(ctx, source)
	result.Discovered = len(candidates)
	result.FeedsFound = feedsFound
	result.LinksFound = linksFound
	if err != nil {
		result.SourceError = err.Error()
		_ = finishDiscoveryFetch(source, result, fetchResult, startedAt, err)
		return result, err
	}

	for _, candidate := range candidates {
		writeResult, err := upsertDiscoveryCandidate(*source, candidate)
		if err != nil {
			_ = finishDiscoveryFetch(source, result, fetchResult, startedAt, err)
			return result, err
		}
		if writeResult.Created {
			result.Stored++
			if err := enqueueCandidateForProcessing(ctx, writeResult.Candidate); err != nil {
				log.Printf("candidate %d processing enqueue failed: %v", writeResult.Candidate.ID, err)
			}
		}
	}
	if err := finishDiscoveryFetch(source, result, fetchResult, startedAt, nil); err != nil {
		return result, err
	}
	return result, nil
}

func StartDiscoveryScheduler() func() {
	interval, err := time.ParseDuration(strings.TrimSpace(config.DISCOVERYFETCHINTERVAL))
	if err != nil || interval <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				queue, available := jobQueue()
				if !available {
					log.Printf("discovery scheduler skipped: shared job queue unavailable")
					continue
				}
				if err := RecoverDueJobs(context.Background(), queue, time.Now()); err != nil {
					log.Printf("discovery scheduler enqueue failed: %v", err)
				}
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

func NormalizeDiscoveryURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", errors.New("url is required")
	}
	parsedURL, err := neturl.Parse(trimmed)
	if err != nil {
		return "", err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", errors.New("only http and https urls are supported")
	}
	if parsedURL.Hostname() == "" {
		return "", errors.New("url host is required")
	}
	parsedURL.Fragment = ""
	return parsedURL.String(), nil
}

func discoverCandidates(ctx context.Context, source *DiscoverySource) ([]discoveredCandidate, int, int, *FetchResult, error) {
	switch normalizeDiscoverySourceType(source.Type) {
	case DiscoverySourceTypeFeed, DiscoverySourceTypeRSSHub:
		fetchResult, err := fetchDiscoveryRequest(ctx, FetchRequest{
			URL: source.URL, ETag: source.ETag, LastModified: source.LastModified,
			Kind: FetchKindFeed,
		})
		if err != nil {
			return nil, 0, 0, nil, err
		}
		if fetchResult.NotModified {
			return nil, 1, 0, &fetchResult, nil
		}
		if fetchResult.StatusCode < 200 || fetchResult.StatusCode >= 300 {
			statusError := fmt.Errorf("%w: %d", ErrHTTPFetchStatus, fetchResult.StatusCode)
			return nil, 0, 0, &fetchResult, withHTTPStatusDiagnostic(statusError, fetchResult, source.URL)
		}
		candidates, err := parseFeedCandidates(fetchResult.Body)
		if err != nil {
			return nil, 0, 0, &fetchResult, withFetchDiagnostic(ErrFeedParse, fetchResult.FinalURLOr(source.URL), fetchResult.StatusCode, "feed_parse")
		}
		for index := range candidates {
			candidates[index].DiscoveryMethod = DiscoveryMethodFeed
			candidates[index].SourcePageURL = source.URL
			candidates[index].MetadataConfidence = metadataConfidenceForMethod(DiscoveryMethodFeed)
			candidates[index].PublishedConfidence = "feed"
		}
		if source.UserManaged {
			candidates = followFeedNextPages(ctx, fetchResult.FinalURLOr(source.URL), fetchResult.Body, candidates)
		}
		return scoreAndLimitCandidates(candidates, source.UserManaged), 1, 0, &fetchResult, nil
	case DiscoverySourceTypeSite:
		site, err := siteForDiscoverySource(*source)
		if err != nil {
			return nil, 0, 0, nil, err
		}
		etag := source.ETag
		lastModified := source.LastModified
		if site.Status == DiscoverySiteStatusObserving {
			etag = ""
			lastModified = ""
		}
		fetchResult, err := fetchDiscoveryRequest(ctx, FetchRequest{
			URL: source.URL, ETag: etag, LastModified: lastModified, Kind: FetchKindHTML,
		})
		if err != nil {
			return nil, 0, 0, nil, err
		}
		if fetchResult.NotModified {
			if site.Status == DiscoverySiteStatusObserving {
				return nil, 0, 0, &fetchResult, ErrBlogVerificationUnavailable
			}
			return nil, 0, 0, &fetchResult, nil
		}
		if fetchResult.StatusCode < 200 || fetchResult.StatusCode >= 300 {
			statusError := fmt.Errorf("%w: %d", ErrHTTPFetchStatus, fetchResult.StatusCode)
			return nil, 0, 0, &fetchResult, withHTTPStatusDiagnostic(statusError, fetchResult, source.URL)
		}
		wasObserving := site.Status == DiscoverySiteStatusObserving
		if wasObserving {
			verification, verifyErr := VerifyBlogHomepage(fetchResult.Body, fetchResult.FinalURLOr(source.URL), site.RootURL)
			if verifyErr != nil {
				return nil, 0, 0, &fetchResult, verifyErr
			}
			if err := persistObservedSiteVerification(site, verification, discoveryClock.Now()); err != nil {
				return nil, 0, 0, &fetchResult, err
			}
			if !verification.IsBlog {
				return nil, 0, 0, &fetchResult, nil
			}
			site.Status = DiscoverySiteStatusActive
		}
		queue, _ := jobQueue()
		service := EndpointDiscoveryService{Clock: discoveryClock, Queue: queue}
		discovered, err := service.DiscoverHomepage(ctx, site, *source, fetchResult.Body, fetchResult.FinalURLOr(source.URL))
		if err != nil {
			return nil, 0, 0, &fetchResult, err
		}
		if wasObserving && queue != nil {
			if err := queue.EnqueueScanBlogroll(ctx, site.ID); err != nil {
				return nil, 0, 0, &fetchResult, err
			}
		}
		return scoreAndLimitCandidates(discovered.Candidates, source.UserManaged), discovered.FeedsFound, discovered.LinksFound, &fetchResult, nil
	default:
		return nil, 0, 0, nil, errors.New("unsupported discovery source type")
	}
}

func fetchSiteCandidates(ctx context.Context, rawURL string) ([]discoveredCandidate, int, int, error) {
	feedURLs, articleURLs, err := crawlSiteLinks(ctx, rawURL)
	if err != nil {
		return nil, 0, 0, err
	}
	feedCandidates := make([]discoveredCandidate, 0)
	for _, feedURL := range feedURLs {
		candidates, err := fetchFeedCandidates(ctx, feedURL)
		if err == nil {
			feedCandidates = append(feedCandidates, candidates...)
		}
	}
	for _, articleURL := range articleURLs {
		feedCandidates = append(feedCandidates, discoveredCandidate{URL: articleURL, Title: articleURL})
	}
	return scoreAndLimitCandidates(feedCandidates, false), len(feedURLs), len(articleURLs), nil
}

func fetchFeedCandidates(ctx context.Context, rawURL string) ([]discoveredCandidate, error) {
	result, err := fetchDiscoveryRequest(ctx, FetchRequest{URL: rawURL, Kind: FetchKindFeed})
	if err != nil {
		return nil, err
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		statusError := fmt.Errorf("%w: %d", ErrHTTPFetchStatus, result.StatusCode)
		return nil, withHTTPStatusDiagnostic(statusError, result, rawURL)
	}
	candidates, err := parseFeedCandidates(result.Body)
	if err != nil {
		return nil, withFetchDiagnostic(ErrFeedParse, result.FinalURLOr(rawURL), result.StatusCode, "feed_parse")
	}
	return candidates, nil
}

func parseFeedCandidates(body []byte) ([]discoveredCandidate, error) {
	parser := gofeed.NewParser()
	parser.UserAgent = strings.TrimSpace(config.DISCOVERYUSERAGENT)
	feed, err := parser.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	candidates := make([]discoveredCandidate, 0, len(feed.Items))
	for _, item := range feed.Items {
		if item == nil {
			continue
		}
		link := strings.TrimSpace(item.Link)
		if link == "" {
			link = strings.TrimSpace(item.GUID)
		}
		if _, rejected := articlerules.RejectURL(link, strings.TrimSpace(item.Title)); rejected {
			continue
		}
		summary := item.Description
		if summary == "" {
			summary = item.Content
		}
		candidates = append(candidates, discoveredCandidate{
			URL:         link,
			Title:       strings.TrimSpace(stripMarkup(item.Title)),
			Summary:     archive.BuildSummary(stripMarkup(summary), 260),
			PublishedAt: firstFeedTime(item.PublishedParsed, item.UpdatedParsed),
		})
	}
	return candidates, nil
}

var feedNextHrefPattern = regexp.MustCompile(`(?is)<link[^>]+>`)

// followFeedNextPages 只对人工订阅跟随 Atom/RSS 的 rel=next，最多 100 页。
func followFeedNextPages(ctx context.Context, pageURL string, body []byte, candidates []discoveredCandidate) []discoveredCandidate {
	seen := make(map[string]struct{}, len(candidates)+8)
	for _, candidate := range candidates {
		seen[candidate.URL] = struct{}{}
	}
	next := feedNextURL(body, pageURL)
	for pages := 0; next != "" && pages < 100; pages++ {
		if _, exists := seen["page:"+next]; exists {
			break
		}
		seen["page:"+next] = struct{}{}
		result, err := fetchDiscoveryRequest(ctx, FetchRequest{URL: next, Kind: FetchKindFeed})
		if err != nil || result.StatusCode < 200 || result.StatusCode >= 300 {
			break
		}
		more, parseErr := parseFeedCandidates(result.Body)
		if parseErr != nil {
			break
		}
		for _, candidate := range more {
			if _, exists := seen[candidate.URL]; exists {
				continue
			}
			seen[candidate.URL] = struct{}{}
			candidate.DiscoveryMethod = DiscoveryMethodFeed
			candidate.SourcePageURL = next
			candidate.MetadataConfidence = metadataConfidenceForMethod(DiscoveryMethodFeed)
			candidate.PublishedConfidence = "feed"
			candidates = append(candidates, candidate)
		}
		next = feedNextURL(result.Body, result.FinalURLOr(next))
	}
	return candidates
}

func feedNextURL(body []byte, pageURL string) string {
	base, err := neturl.Parse(pageURL)
	if err != nil {
		return ""
	}
	for _, raw := range feedNextHrefPattern.FindAll(body, -1) {
		tag := strings.ToLower(string(raw))
		if !strings.Contains(tag, `rel="next"`) && !strings.Contains(tag, `rel='next'`) {
			continue
		}
		href := attrValueFromTag(string(raw), "href")
		if href == "" {
			continue
		}
		resolved, ok := resolveWebURL(href, base)
		if !ok {
			continue
		}
		return resolved.String()
	}
	return ""
}

func attrValueFromTag(tag string, name string) string {
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(name) + `\s*=\s*("([^"]*)"|'([^']*)')`)
	match := pattern.FindStringSubmatch(tag)
	if len(match) >= 4 {
		if match[2] != "" {
			return match[2]
		}
		return match[3]
	}
	return ""
}

func crawlSiteLinks(ctx context.Context, rawURL string) ([]string, []string, error) {
	baseURL, err := neturl.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	feedSet := make(map[string]struct{})
	articleSet := make(map[string]struct{})
	maxPages := config.DISCOVERYMAXCANDIDATES
	if maxPages <= 0 {
		maxPages = 50
	}

	queue := []string{rawURL}
	seen := make(map[string]struct{})
	for len(queue) > 0 && len(seen) < maxPages {
		pageURL := queue[0]
		queue = queue[1:]
		if _, exists := seen[pageURL]; exists {
			continue
		}
		seen[pageURL] = struct{}{}
		result, fetchErr := fetchDiscoveryRequest(ctx, FetchRequest{URL: pageURL, Kind: FetchKindHTML})
		if fetchErr != nil || result.StatusCode < 200 || result.StatusCode >= 300 {
			if pageURL == rawURL {
				if fetchErr != nil {
					return nil, nil, fetchErr
				}
				statusError := fmt.Errorf("%w: %d", ErrHTTPFetchStatus, result.StatusCode)
				return nil, nil, withHTTPStatusDiagnostic(statusError, result, pageURL)
			}
			continue
		}
		feeds, articles := discoverLinksFromHTML(result.Body, baseURL)
		for _, feedURL := range feeds {
			feedSet[feedURL] = struct{}{}
		}
		for _, articleURL := range articles {
			articleSet[articleURL] = struct{}{}
			if len(seen)+len(queue) < maxPages {
				queue = append(queue, articleURL)
			}
		}
	}
	return sortedKeys(feedSet), sortedKeys(articleSet), nil
}

func parseRSSCandidates(body []byte) []discoveredCandidate {
	var feed struct {
		Channel struct {
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
				PubDate     string `xml:"pubDate"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil
	}
	candidates := make([]discoveredCandidate, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		if _, rejected := articlerules.RejectURL(item.Link, item.Title); rejected {
			continue
		}
		candidates = append(candidates, discoveredCandidate{
			URL:         strings.TrimSpace(item.Link),
			Title:       strings.TrimSpace(stripMarkup(item.Title)),
			Summary:     archive.BuildSummary(stripMarkup(item.Description), 260),
			PublishedAt: parseFeedTime(item.PubDate),
		})
	}
	return candidates
}

func parseAtomCandidates(body []byte) []discoveredCandidate {
	var feed struct {
		Entries []struct {
			Title   string `xml:"title"`
			Summary string `xml:"summary"`
			Content string `xml:"content"`
			Updated string `xml:"updated"`
			Links   []struct {
				Href string `xml:"href,attr"`
				Rel  string `xml:"rel,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil
	}
	candidates := make([]discoveredCandidate, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		link := ""
		for _, atomLink := range entry.Links {
			if atomLink.Rel == "" || atomLink.Rel == "alternate" {
				link = atomLink.Href
				break
			}
		}
		summary := entry.Summary
		if summary == "" {
			summary = entry.Content
		}
		if _, rejected := articlerules.RejectURL(link, entry.Title); rejected {
			continue
		}
		candidates = append(candidates, discoveredCandidate{
			URL:         strings.TrimSpace(link),
			Title:       strings.TrimSpace(stripMarkup(entry.Title)),
			Summary:     archive.BuildSummary(stripMarkup(summary), 260),
			PublishedAt: parseFeedTime(entry.Updated),
		})
	}
	return candidates
}

func discoverLinksFromHTML(body []byte, baseURL *neturl.URL) ([]string, []string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, nil
	}
	feedSet := make(map[string]struct{})
	articleSet := make(map[string]struct{})
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "link":
				href := attrValue(node, "href")
				rel := strings.ToLower(attrValue(node, "rel"))
				linkType := strings.ToLower(attrValue(node, "type"))
				if href != "" && strings.Contains(rel, "alternate") && isFeedMediaType(linkType) {
					if absoluteURL, ok := sameHostURL(href, baseURL); ok {
						feedSet[absoluteURL] = struct{}{}
					}
				}
			case "a":
				href := attrValue(node, "href")
				if absoluteURL, ok := sameHostArticleURL(href, baseURL); ok {
					if _, rejected := articlerules.RejectURL(absoluteURL, compactNodeText(node, 160)); rejected {
						break
					}
					articleSet[absoluteURL] = struct{}{}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return sortedKeys(feedSet), sortedKeys(articleSet)
}

// scoreAndLimitCandidates 去重并排序。unlimited 为 true 时不截断（人工订阅全量入池）。
func scoreAndLimitCandidates(candidates []discoveredCandidate, unlimited bool) []discoveredCandidate {
	seen := make(map[string]discoveredCandidate)
	for _, candidate := range candidates {
		normalizedURL, err := NormalizeDiscoveryURL(candidate.URL)
		if err != nil {
			continue
		}
		if _, rejected := articlerules.RejectURL(normalizedURL, candidate.Title); rejected {
			continue
		}
		candidate.URL = normalizedURL
		if existing, ok := seen[normalizedURL]; ok && scoreDiscoveredCandidate(existing) >= scoreDiscoveredCandidate(candidate) {
			continue
		}
		seen[normalizedURL] = candidate
	}
	result := make([]discoveredCandidate, 0, len(seen))
	for _, candidate := range seen {
		result = append(result, candidate)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return scoreDiscoveredCandidate(result[i]) > scoreDiscoveredCandidate(result[j])
	})
	if unlimited {
		return result
	}
	limit := config.DISCOVERYMAXCANDIDATES
	if limit <= 0 {
		limit = 50
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func scoreDiscoveredCandidate(candidate discoveredCandidate) float64 {
	score := 1.0
	if candidate.Title != "" {
		score += 2
	}
	if candidate.Summary != "" {
		score += 1
	}
	if candidate.PublishedAt != nil {
		ageHours := math.Max(time.Since(*candidate.PublishedAt).Hours(), 0)
		if ageHours < 24 {
			score += 4
		} else if ageHours < 24*7 {
			score += 2
		} else if ageHours < 24*30 {
			score += 1
		}
	}
	keywords, err := archive.GetKeywordStats("", "30d", 20)
	if err == nil {
		text := strings.ToLower(candidate.Title + " " + candidate.Summary + " " + candidate.URL)
		for _, keyword := range keywords {
			if keyword.Keyword != "" && strings.Contains(text, strings.ToLower(keyword.Keyword)) {
				score += float64(keyword.Count)
			}
		}
	}
	return score
}

func normalizeLimit(limit int, defaultLimit int, maxLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func sameHostArticleURL(rawURL string, baseURL *neturl.URL) (string, bool) {
	absoluteURL, ok := sameHostURL(rawURL, baseURL)
	if !ok {
		return "", false
	}
	parsedURL, err := neturl.Parse(absoluteURL)
	if err != nil {
		return "", false
	}
	path := strings.ToLower(parsedURL.Path)
	if path == "" || path == "/" {
		return "", false
	}
	if strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".gif") || strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".pdf") {
		return "", false
	}
	if strings.Contains(path, "blog") || strings.Contains(path, "post") || strings.Contains(path, "article") || strings.Contains(path, "news") || strings.Count(strings.Trim(path, "/"), "/") >= 1 {
		return absoluteURL, true
	}
	return "", false
}

func sameHostURL(rawURL string, baseURL *neturl.URL) (string, bool) {
	if strings.TrimSpace(rawURL) == "" || strings.HasPrefix(rawURL, "#") || strings.HasPrefix(strings.ToLower(rawURL), "mailto:") || strings.HasPrefix(strings.ToLower(rawURL), "javascript:") {
		return "", false
	}
	parsedURL, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", false
	}
	resolvedURL := baseURL.ResolveReference(parsedURL)
	if !strings.EqualFold(resolvedURL.Hostname(), baseURL.Hostname()) {
		return "", false
	}
	if resolvedURL.Scheme != "http" && resolvedURL.Scheme != "https" {
		return "", false
	}
	resolvedURL.Fragment = ""
	return resolvedURL.String(), true
}

func normalizeDiscoverySourceType(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case DiscoverySourceTypeFeed, "rss", "atom":
		return DiscoverySourceTypeFeed
	case DiscoverySourceTypeRSSHub:
		return DiscoverySourceTypeRSSHub
	case DiscoverySourceTypeSite, "crawler":
		return DiscoverySourceTypeSite
	case DiscoverySourceTypeSitemap:
		return DiscoverySourceTypeSitemap
	default:
		return DiscoverySourceTypeFeed
	}
}

func siteForDiscoverySource(source DiscoverySource) (DiscoverySite, error) {
	if db == nil || source.SiteID == nil {
		return DiscoverySite{}, errors.New("discovery endpoint is not attached to a site")
	}
	var site DiscoverySite
	if err := db.First(&site, *source.SiteID).Error; err != nil {
		return site, err
	}
	return site, nil
}

func firstFeedTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func parseFeedTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	layouts := []string{time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, "2006-01-02"}
	for _, layout := range layouts {
		if parsedTime, err := time.Parse(layout, value); err == nil {
			return &parsedTime
		}
	}
	return nil
}

func stripMarkup(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return strings.TrimSpace(value)
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			builder.WriteString(node.Data)
			builder.WriteString(" ")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return strings.Join(strings.Fields(builder.String()), " ")
}

func attrValue(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hostLabel(rawURL string) string {
	parsedURL, err := neturl.Parse(rawURL)
	if err != nil || parsedURL.Hostname() == "" {
		return rawURL
	}
	return parsedURL.Hostname()
}
