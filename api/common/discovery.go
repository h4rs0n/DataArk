package common

import (
	discoveryguard "DataArk/discovery"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
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

	discoveryMaxBodyBytes = 4 << 20
)

var validateDiscoveryFetchURL = discoveryguard.ValidateFetchURL
var fetchDiscoveryBody = fetchDiscoveryURL

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
		Name:    strings.TrimSpace(name),
		URL:     normalizedURL,
		Type:    sourceType,
		Enabled: enabled,
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
		"name":    strings.TrimSpace(name),
		"url":     normalizedURL,
		"type":    normalizeDiscoverySourceType(sourceType),
		"enabled": enabled,
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
	candidates, feedsFound, linksFound, err := discoverCandidates(ctx, source)
	result.Discovered = len(candidates)
	result.FeedsFound = feedsFound
	result.LinksFound = linksFound
	now := time.Now()

	if err != nil {
		result.SourceError = err.Error()
		if db != nil {
			_ = db.Model(source).Updates(map[string]interface{}{
				"last_fetched_at": &now,
				"last_error":      err.Error(),
			}).Error
		}
		return result, err
	}

	for _, candidate := range candidates {
		if err := upsertDiscoveryCandidate(*source, candidate); err != nil {
			return result, err
		}
		result.Stored++
	}
	if db != nil {
		if err := db.Model(source).Updates(map[string]interface{}{
			"last_fetched_at": &now,
			"last_error":      "",
		}).Error; err != nil {
			return result, err
		}
	}
	return result, nil
}

func StartDiscoveryScheduler() func() {
	interval, err := time.ParseDuration(strings.TrimSpace(DISCOVERYFETCHINTERVAL))
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
				if err := FetchEnabledDiscoverySources(context.Background()); err != nil {
					log.Printf("discovery scheduler fetch failed: %v", err)
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

func discoverCandidates(ctx context.Context, source *DiscoverySource) ([]discoveredCandidate, int, int, error) {
	switch normalizeDiscoverySourceType(source.Type) {
	case DiscoverySourceTypeFeed, DiscoverySourceTypeRSSHub:
		candidates, err := fetchFeedCandidates(ctx, source.URL)
		return scoreAndLimitCandidates(candidates), 1, 0, err
	case DiscoverySourceTypeSite:
		return fetchSiteCandidates(ctx, source.URL)
	default:
		return nil, 0, 0, errors.New("unsupported discovery source type")
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
	body, _, err := fetchDiscoveryBody(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return parseFeedCandidates(body)
}

func parseFeedCandidates(body []byte) ([]discoveredCandidate, error) {
	parser := gofeed.NewParser()
	parser.UserAgent = strings.TrimSpace(DISCOVERYUSERAGENT)
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
			Summary:     BuildSummary(stripMarkup(summary), 260),
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
	body, _, err := fetchDiscoveryURL(ctx, sitemapURL.String())
	if err != nil {
		return nil, err
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
	if err := xml.Unmarshal(body, &sitemap); err != nil {
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

func fetchDiscoveryURL(ctx context.Context, rawURL string) ([]byte, string, error) {
	timeout, err := time.ParseDuration(strings.TrimSpace(DISCOVERYREQUESTTIMEOUT))
	if err != nil || timeout <= 0 {
		timeout = 12 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	validatedURL, err := validateDiscoveryFetchURL(requestCtx, rawURL)
	if err != nil {
		return nil, "", err
	}

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, validatedURL.String(), nil)
	if err != nil {
		return nil, "", err
	}
	userAgent := strings.TrimSpace(DISCOVERYUSERAGENT)
	if userAgent == "" {
		userAgent = "DataArkDiscovery/1.0"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, text/html;q=0.9, */*;q=0.1")

	redirects := 0
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirects++
			if redirects > 5 {
				return errors.New("too many discovery redirects")
			}
			_, err := validateDiscoveryFetchURL(requestCtx, req.URL.String())
			return err
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("discovery request returned status %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(resp.Body, discoveryMaxBodyBytes))
	if err != nil {
		return nil, "", err
	}
	return body, contentType, nil
}

func crawlSiteLinks(ctx context.Context, rawURL string) ([]string, []string, error) {
	baseURL, err := neturl.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	if _, err := validateDiscoveryFetchURL(ctx, rawURL); err != nil {
		return nil, nil, err
	}

	feedSet := make(map[string]struct{})
	articleSet := make(map[string]struct{})
	timeout, err := time.ParseDuration(strings.TrimSpace(DISCOVERYREQUESTTIMEOUT))
	if err != nil || timeout <= 0 {
		timeout = 12 * time.Second
	}
	userAgent := strings.TrimSpace(DISCOVERYUSERAGENT)
	if userAgent == "" {
		userAgent = "DataArkDiscovery/1.0"
	}
	maxPages := DISCOVERYMAXCANDIDATES
	if maxPages <= 0 {
		maxPages = 50
	}

	collector := colly.NewCollector(
		colly.AllowedDomains(baseURL.Hostname()),
		colly.MaxDepth(2),
		colly.UserAgent(userAgent),
	)
	collector.SetRequestTimeout(timeout)
	_ = collector.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: 2, Delay: time.Second})

	visited := 0
	var firstErr error
	collector.OnRequest(func(request *colly.Request) {
		if _, err := validateDiscoveryFetchURL(ctx, request.URL.String()); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			request.Abort()
			return
		}
		visited++
		if visited > maxPages {
			request.Abort()
		}
	})
	collector.OnHTML("link[href]", func(element *colly.HTMLElement) {
		rel := strings.ToLower(element.Attr("rel"))
		linkType := strings.ToLower(element.Attr("type"))
		if strings.Contains(rel, "alternate") && (strings.Contains(linkType, "rss") || strings.Contains(linkType, "atom") || strings.Contains(linkType, "json") || strings.Contains(linkType, "xml")) {
			if absoluteURL, ok := sameHostURL(element.Attr("href"), baseURL); ok {
				feedSet[absoluteURL] = struct{}{}
			}
		}
	})
	collector.OnHTML("a[href]", func(element *colly.HTMLElement) {
		href := element.Attr("href")
		if absoluteURL, ok := sameHostArticleURL(href, baseURL); ok {
			articleSet[absoluteURL] = struct{}{}
		}
		if visited < maxPages {
			_ = element.Request.Visit(href)
		}
	})

	if err := collector.Visit(rawURL); err != nil {
		return nil, nil, err
	}
	collector.Wait()
	if firstErr != nil && len(feedSet) == 0 && len(articleSet) == 0 {
		return nil, nil, firstErr
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
			Summary:     BuildSummary(stripMarkup(item.Description), 260),
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
			Summary:     BuildSummary(stripMarkup(summary), 260),
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
	articleURL, err := discoveryguard.NormalizeArticleURL(normalizedURL)
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
		Summary:          BuildSummary(candidate.Summary, 260),
		Status:           DiscoveryCandidateStatusNew,
		EnrichmentStatus: RecommendationEnrichmentStatusPending,
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
	limit := DISCOVERYMAXCANDIDATES
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
	keywords, err := GetKeywordStats("", "30d", 20)
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
