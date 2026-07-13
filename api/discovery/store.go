package discovery

import (
	"DataArk/archive"
	"DataArk/config"
	"DataArk/jobqueue"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"math"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	URL         string
	Title       string
	Summary     string
	PublishedAt *time.Time
}

func ListDiscoverySources() ([]DiscoverySource, error) {
	sources := make([]DiscoverySource, 0)
	if db == nil {
		return sources, nil
	}
	err := db.Order("created_at desc").Find(&sources).Error
	return sources, err
}

func CreateDiscoverySource(name string, rawURL string, sourceType string, enabled bool) (*DiscoverySource, error) {
	normalizedURL, err := NormalizeDiscoveryURL(rawURL)
	if err != nil {
		return nil, err
	}
	sourceType = normalizeDiscoverySourceType(sourceType)
	if strings.TrimSpace(name) == "" {
		name = hostLabel(normalizedURL)
	}
	source := &DiscoverySource{
		Name:         strings.TrimSpace(name),
		URL:          normalizedURL,
		Type:         sourceType,
		EndpointType: legacyEndpointType(sourceType),
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
	if err := db.Create(source).Error; err != nil {
		return nil, err
	}
	return source, nil
}

func UpdateDiscoverySource(id uint, name string, rawURL string, sourceType string, enabled bool) (*DiscoverySource, error) {
	if db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var source DiscoverySource
	if err := db.First(&source, id).Error; err != nil {
		return nil, err
	}
	normalizedURL, err := NormalizeDiscoveryURL(rawURL)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"name":          strings.TrimSpace(name),
		"url":           normalizedURL,
		"type":          normalizeDiscoverySourceType(sourceType),
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
	return db.Delete(&DiscoverySource{}, id).Error
}

func ListDiscoveryCandidates(status string, limit int) ([]DiscoveryCandidate, error) {
	candidates := make([]DiscoveryCandidate, 0)
	if db == nil {
		return candidates, nil
	}
	limit = normalizeLimit(limit, 50, 200)
	query := db.Order("score desc, published_at desc, last_seen_at desc").Limit(limit)
	if strings.TrimSpace(status) != "" {
		query = query.Where("status = ?", strings.TrimSpace(status))
	}
	err := query.Find(&candidates).Error
	return candidates, err
}

func MarkDiscoveryCandidateRead(id uint) (*DiscoveryCandidate, error) {
	return updateCandidateStatus(id, DiscoveryCandidateStatusRead, "", "read")
}

func MarkDiscoveryCandidateIgnored(id uint) (*DiscoveryCandidate, error) {
	return updateCandidateStatus(id, DiscoveryCandidateStatusIgnored, "", "ignore")
}

func MarkDiscoveryCandidateArchived(id uint, taskID string) (*DiscoveryCandidate, error) {
	return updateCandidateStatus(id, DiscoveryCandidateStatusArchived, taskID, "archive")
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

func FetchDiscoverySource(ctx context.Context, source *DiscoverySource) (*DiscoveryFetchResult, error) {
	if source == nil {
		return nil, errors.New("missing discovery source")
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
		if err := upsertDiscoveryCandidate(*source, candidate); err != nil {
			_ = finishDiscoveryFetch(source, result, fetchResult, startedAt, err)
			return result, err
		}
		result.Stored++
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
				queue, available := jobqueue.Default()
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

func FetchEnabledDiscoverySources(ctx context.Context) error {
	if db == nil {
		return nil
	}
	var sources []DiscoverySource
	if err := db.Where("enabled = ?", true).Order("last_fetched_at asc").Find(&sources).Error; err != nil {
		return err
	}
	for index := range sources {
		if _, err := FetchDiscoverySource(ctx, &sources[index]); err != nil {
			log.Printf("failed to fetch discovery source %d: %v", sources[index].ID, err)
		}
	}
	return nil
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
			return nil, 0, 0, &fetchResult, fmt.Errorf("%w: %d", ErrHTTPFetchStatus, fetchResult.StatusCode)
		}
		candidates, err := parseFeedCandidates(fetchResult.Body)
		return scoreAndLimitCandidates(candidates), 1, 0, &fetchResult, err
	case DiscoverySourceTypeSite:
		candidates, feeds, links, err := fetchSiteCandidates(ctx, source.URL)
		return candidates, feeds, links, nil, err
	default:
		return nil, 0, 0, nil, errors.New("unsupported discovery source type")
	}
}

func fetchSiteCandidates(ctx context.Context, rawURL string) ([]discoveredCandidate, int, int, error) {
	feedURLs, articleURLs, err := crawlSiteLinks(ctx, rawURL)
	if err != nil {
		return nil, 0, 0, err
	}
	baseURL, err := neturl.Parse(rawURL)
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
	sitemapCandidates, _ := fetchSitemapCandidates(ctx, baseURL)
	for _, articleURL := range articleURLs {
		feedCandidates = append(feedCandidates, discoveredCandidate{URL: articleURL, Title: articleURL})
	}
	feedCandidates = append(feedCandidates, sitemapCandidates...)
	return scoreAndLimitCandidates(feedCandidates), len(feedURLs), len(articleURLs), nil
}

func fetchFeedCandidates(ctx context.Context, rawURL string) ([]discoveredCandidate, error) {
	result, err := fetchDiscoveryRequest(ctx, FetchRequest{URL: rawURL, Kind: FetchKindFeed})
	if err != nil {
		return nil, err
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: %d", ErrHTTPFetchStatus, result.StatusCode)
	}
	return parseFeedCandidates(result.Body)
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

func fetchSitemapCandidates(ctx context.Context, baseURL *neturl.URL) ([]discoveredCandidate, error) {
	sitemapURL := *baseURL
	sitemapURL.Path = "/sitemap.xml"
	sitemapURL.RawQuery = ""
	sitemapURL.Fragment = ""
	result, err := fetchDiscoveryRequest(ctx, FetchRequest{URL: sitemapURL.String(), Kind: FetchKindSitemap})
	if err != nil {
		return nil, err
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: %d", ErrHTTPFetchStatus, result.StatusCode)
	}

	var sitemap struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
		Sitemaps []struct {
			Loc string `xml:"loc"`
		} `xml:"sitemap"`
	}
	if err := xml.Unmarshal(result.Body, &sitemap); err != nil {
		return nil, err
	}
	candidates := make([]discoveredCandidate, 0)
	for _, item := range sitemap.URLs {
		normalizedURL, ok := sameHostArticleURL(item.Loc, baseURL)
		if !ok {
			continue
		}
		candidate := discoveredCandidate{URL: normalizedURL, Title: normalizedURL}
		if parsedTime := parseFeedTime(item.LastMod); parsedTime != nil {
			candidate.PublishedAt = parsedTime
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
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
				return nil, nil, fmt.Errorf("%w: %d", ErrHTTPFetchStatus, result.StatusCode)
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
				if href != "" && strings.Contains(rel, "alternate") && (strings.Contains(linkType, "rss") || strings.Contains(linkType, "atom") || strings.Contains(linkType, "xml")) {
					if absoluteURL, ok := sameHostURL(href, baseURL); ok {
						feedSet[absoluteURL] = struct{}{}
					}
				}
			case "a":
				href := attrValue(node, "href")
				if absoluteURL, ok := sameHostArticleURL(href, baseURL); ok {
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

func upsertDiscoveryCandidate(source DiscoverySource, candidate discoveredCandidate) error {
	normalizedURL, err := NormalizeDiscoveryURL(candidate.URL)
	if err != nil {
		return nil
	}
	articleURL, err := NormalizeArticleURL(normalizedURL)
	if err != nil {
		articleURL = normalizedURL
	}
	title := strings.TrimSpace(candidate.Title)
	if title == "" {
		title = normalizedURL
	}
	now := time.Now()
	record := DiscoveryCandidate{
		SourceID:         source.ID,
		SourceName:       source.Name,
		URL:              normalizedURL,
		NormalizedURL:    articleURL,
		CanonicalURL:     articleURL,
		Title:            title,
		Summary:          archive.BuildSummary(candidate.Summary, 260),
		Status:           DiscoveryCandidateStatusNew,
		EnrichmentStatus: DiscoveryCandidateEnrichmentStatusPending,
		DedupeKey:        articleURL,
		Score:            scoreDiscoveredCandidate(candidate),
		PublishedAt:      candidate.PublishedAt,
		LastSeenAt:       now,
	}
	if db == nil {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "url"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"source_id":         source.ID,
			"source_name":       source.Name,
			"normalized_url":    record.NormalizedURL,
			"canonical_url":     record.CanonicalURL,
			"title":             record.Title,
			"summary":           record.Summary,
			"score":             record.Score,
			"published_at":      record.PublishedAt,
			"enrichment_status": record.EnrichmentStatus,
			"dedupe_key":        record.DedupeKey,
			"last_seen_at":      now,
			"updated_at":        now,
		}),
	}).Create(&record).Error
}

func updateCandidateStatus(id uint, status string, taskID string, action string) (*DiscoveryCandidate, error) {
	if db == nil {
		return nil, gorm.ErrRecordNotFound
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, id).Error; err != nil {
		return nil, err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{"status": status}
		if taskID != "" {
			updates["archived_task_id"] = taskID
		}
		if err := tx.Model(&candidate).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Create(&DiscoveryCandidateFeedback{CandidateID: id, Action: action}).Error
	}); err != nil {
		return nil, err
	}
	if err := db.First(&candidate, id).Error; err != nil {
		return nil, err
	}
	return &candidate, nil
}

func scoreAndLimitCandidates(candidates []discoveredCandidate) []discoveredCandidate {
	seen := make(map[string]discoveredCandidate)
	for _, candidate := range candidates {
		normalizedURL, err := NormalizeDiscoveryURL(candidate.URL)
		if err != nil {
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
	default:
		return DiscoverySourceTypeFeed
	}
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
