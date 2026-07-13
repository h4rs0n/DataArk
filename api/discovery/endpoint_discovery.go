package discovery

import (
	"DataArk/config"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"gorm.io/gorm"
)

const (
	DiscoverySourceTypeSitemap = "sitemap"
	DiscoveryEndpointFeed      = "feed"
	DiscoveryEndpointSitemap   = "sitemap"
	maxNestedSitemaps          = 50
)

type EndpointDiscoveryService struct {
	Clock Clock
	Queue JobEnqueuer
}

type EndpointDiscoveryResult struct {
	FeedsFound    int
	SitemapsFound int
	LinksFound    int
	Candidates    []discoveredCandidate
}

type endpointLinkSet struct {
	feeds      []string
	sitemaps   []string
	candidates []discoveredCandidate
}

func (service EndpointDiscoveryService) DiscoverHomepage(ctx context.Context, site DiscoverySite, homepageSource DiscoverySource, body []byte, pageURL string) (EndpointDiscoveryResult, error) {
	result := EndpointDiscoveryResult{Candidates: make([]discoveredCandidate, 0)}
	clock := service.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	links, err := discoverHomepageEndpoints(body, pageURL, site.RootURL)
	if err != nil {
		return result, err
	}
	result.Candidates = append(result.Candidates, links.candidates...)
	result.LinksFound = len(links.candidates)
	feedURLs := append([]string(nil), links.feeds...)
	sitemapURLs := append([]string(nil), links.sitemaps...)

	if len(feedURLs) == 0 {
		for _, rawURL := range commonEndpointURLs(site.RootURL, []string{"/feed.xml", "/feed", "/rss.xml", "/atom.xml", "/index.xml"}) {
			response, fetchErr := fetchDiscoveryRequest(ctx, FetchRequest{URL: rawURL, Kind: FetchKindFeed})
			if fetchErr == nil && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
				if _, parseErr := parseFeedCandidates(response.Body); parseErr == nil {
					feedURLs = append(feedURLs, rawURL)
					break
				}
			}
		}
	}

	robotsURL := originURL(site.RootURL) + "/robots.txt"
	if response, fetchErr := fetchDiscoveryRequest(ctx, FetchRequest{URL: robotsURL, Kind: FetchKindRobots}); fetchErr == nil && response.StatusCode == http.StatusOK {
		sitemapURLs = append(sitemapURLs, sitemapURLsFromRobots(response.Body)...)
	}
	if len(sitemapURLs) == 0 {
		defaultSitemap := originURL(site.RootURL) + "/sitemap.xml"
		response, fetchErr := fetchDiscoveryRequest(ctx, FetchRequest{URL: defaultSitemap, Kind: FetchKindSitemap})
		if fetchErr == nil && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			if _, _, parseErr := parseSitemapDocument(response.Body, site.RootURL); parseErr == nil {
				sitemapURLs = append(sitemapURLs, defaultSitemap)
			}
		}
	}
	if rssHubURL := configuredRSSHubEndpoint(homepageSource.CrawlConfig); rssHubURL != "" {
		feedURLs = append(feedURLs, rssHubURL)
	}

	var enqueueErrors []error
	for _, rawURL := range uniqueNormalizedURLs(feedURLs) {
		sourceType := DiscoverySourceTypeFeed
		endpointType := DiscoveryEndpointFeed
		if strings.TrimRight(rawURL, "/") == strings.TrimRight(configuredRSSHubEndpoint(homepageSource.CrawlConfig), "/") && configuredRSSHubEndpoint(homepageSource.CrawlConfig) != "" {
			sourceType = DiscoverySourceTypeRSSHub
			endpointType = DiscoverySourceTypeRSSHub
		}
		endpoint, created, saveErr := upsertSiteEndpoint(site, rawURL, sourceType, endpointType, clock.Now())
		if saveErr != nil {
			return result, saveErr
		}
		result.FeedsFound++
		if created && service.Queue != nil {
			enqueueErrors = appendIfError(enqueueErrors, service.Queue.EnqueueFetchSource(ctx, endpoint.ID))
		}
	}
	for _, rawURL := range uniqueNormalizedURLs(sitemapURLs) {
		endpoint, created, saveErr := upsertSiteEndpoint(site, rawURL, DiscoverySourceTypeSitemap, DiscoveryEndpointSitemap, clock.Now())
		if saveErr != nil {
			return result, saveErr
		}
		result.SitemapsFound++
		if created && service.Queue != nil {
			enqueueErrors = appendIfError(enqueueErrors, service.Queue.EnqueueFetchSource(ctx, endpoint.ID))
		}
	}
	return result, errors.Join(enqueueErrors...)
}

func (service EndpointDiscoveryService) SaveNestedSitemaps(ctx context.Context, site DiscoverySite, urls []string) error {
	clock := service.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	var enqueueErrors []error
	for index, rawURL := range uniqueNormalizedURLs(urls) {
		if index >= maxNestedSitemaps {
			break
		}
		endpoint, created, err := upsertSiteEndpoint(site, rawURL, DiscoverySourceTypeSitemap, DiscoveryEndpointSitemap, clock.Now())
		if err != nil {
			return err
		}
		if created && service.Queue != nil {
			enqueueErrors = appendIfError(enqueueErrors, service.Queue.EnqueueFetchSource(ctx, endpoint.ID))
		}
	}
	return errors.Join(enqueueErrors...)
}

func discoverHomepageEndpoints(body []byte, pageURL string, siteRootURL string) (endpointLinkSet, error) {
	document, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return endpointLinkSet{}, err
	}
	baseURL, err := neturl.Parse(pageURL)
	if err != nil {
		return endpointLinkSet{}, err
	}
	rootURL, err := neturl.Parse(siteRootURL)
	if err != nil {
		return endpointLinkSet{}, err
	}
	feeds := make(map[string]struct{})
	sitemaps := make(map[string]struct{})
	candidates := make(map[string]discoveredCandidate)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "link":
				href := attrValue(node, "href")
				rel := strings.ToLower(attrValue(node, "rel"))
				linkType := strings.ToLower(attrValue(node, "type"))
				if resolved, ok := resolveWebURL(href, baseURL); ok {
					switch {
					case strings.Contains(rel, "alternate") && isFeedMediaType(linkType):
						feeds[resolved.String()] = struct{}{}
					case strings.Contains(rel, "sitemap"):
						sitemaps[resolved.String()] = struct{}{}
					}
				}
			case "a":
				if articleURL, ok := sameHostArticleURL(attrValue(node, "href"), rootURL); ok {
					if isNonArticleNavigation(articleURL, compactNodeText(node, 160)) {
						break
					}
					candidates[articleURL] = discoveredCandidate{
						URL: articleURL, Title: compactNodeText(node, 160),
						DiscoveryMethod: DiscoveryMethodHomepageLink, SourcePageURL: pageURL,
						MetadataConfidence: metadataConfidenceForMethod(DiscoveryMethodHomepageLink),
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	result := endpointLinkSet{feeds: sortedKeys(feeds), sitemaps: sortedKeys(sitemaps), candidates: make([]discoveredCandidate, 0, len(candidates))}
	for _, candidate := range candidates {
		result.candidates = append(result.candidates, candidate)
	}
	return result, nil
}

func isNonArticleNavigation(rawURL string, anchorText string) bool {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return true
	}
	path := strings.ToLower(parsed.Path)
	anchor := strings.ToLower(strings.TrimSpace(anchorText))
	for _, marker := range []string{"/archive", "/archives", "/tag/", "/tags/", "/category/", "/categories/", "/page/", "/about", "/login", "/signin", "/feed", "/rss", "/atom", "/sitemap", "/blogroll", "/friends", "/links"} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	for _, marker := range []string{"archive", "archives", "blogroll", "friends", "links", "about", "sign in", "login"} {
		if anchor == marker {
			return true
		}
	}
	return false
}

func parseSitemapDocument(body []byte, siteRootURL string) ([]discoveredCandidate, []string, error) {
	var sitemap struct {
		XMLName xml.Name
		URLs    []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
		Sitemaps []struct {
			Loc string `xml:"loc"`
		} `xml:"sitemap"`
	}
	if err := xml.Unmarshal(body, &sitemap); err != nil {
		return nil, nil, err
	}
	if sitemap.XMLName.Local != "urlset" && sitemap.XMLName.Local != "sitemapindex" {
		return nil, nil, fmt.Errorf("unsupported sitemap root %q", sitemap.XMLName.Local)
	}
	baseURL, err := neturl.Parse(siteRootURL)
	if err != nil {
		return nil, nil, err
	}
	candidates := make([]discoveredCandidate, 0, len(sitemap.URLs))
	for _, item := range sitemap.URLs {
		normalizedURL, ok := sameHostArticleURL(item.Loc, baseURL)
		if !ok {
			continue
		}
		candidates = append(candidates, discoveredCandidate{
			URL: normalizedURL, PublishedAt: parseFeedTime(item.LastMod),
			DiscoveryMethod: DiscoveryMethodSitemap, MetadataConfidence: metadataConfidenceForMethod(DiscoveryMethodSitemap),
		})
	}
	nested := make([]string, 0, len(sitemap.Sitemaps))
	for _, item := range sitemap.Sitemaps {
		if resolved, ok := resolveWebURL(item.Loc, baseURL); ok && sameLogicalHost(resolved, baseURL) {
			nested = append(nested, resolved.String())
		}
	}
	return candidates, uniqueNormalizedURLs(nested), nil
}

func upsertSiteEndpoint(site DiscoverySite, rawURL string, sourceType string, endpointType string, now time.Time) (DiscoverySource, bool, error) {
	var endpoint DiscoverySource
	normalizedURL, err := NormalizeDiscoveryURL(rawURL)
	if err != nil {
		return endpoint, false, err
	}
	if db == nil {
		return endpoint, false, nil
	}
	created := false
	err = db.Transaction(func(tx *gorm.DB) error {
		findErr := tx.Where("url = ?", normalizedURL).First(&endpoint).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			endpoint = DiscoverySource{
				Name: firstNonBlank(site.DisplayName, hostLabel(normalizedURL)), URL: normalizedURL,
				Type: sourceType, SiteID: &site.ID, EndpointType: endpointType,
				Enabled: true, NextFetchAt: &now, NextDueAt: &now,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&endpoint).Error; err != nil {
				return err
			}
			created = true
			return nil
		}
		if findErr != nil {
			return findErr
		}
		updates := map[string]interface{}{"site_id": site.ID, "type": sourceType, "endpoint_type": endpointType, "enabled": true}
		if endpoint.NextDueAt == nil {
			updates["next_due_at"] = &now
			updates["next_fetch_at"] = &now
		}
		return tx.Model(&endpoint).Updates(updates).Error
	})
	return endpoint, created, err
}

func commonEndpointURLs(rootURL string, paths []string) []string {
	root, err := neturl.Parse(rootURL)
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(paths))
	for _, item := range paths {
		candidate := *root
		candidate.Path = item
		candidate.RawQuery = ""
		candidate.Fragment = ""
		result = append(result, candidate.String())
	}
	return result
}

func originURL(rawURL string) string {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return strings.TrimRight(rawURL, "/")
	}
	return parsed.Scheme + "://" + parsed.Host
}

func sitemapURLsFromRobots(body []byte) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "sitemap") {
			result = append(result, strings.TrimSpace(parts[1]))
		}
	}
	return uniqueNormalizedURLs(result)
}

func uniqueNormalizedURLs(values []string) []string {
	seen := make(map[string]struct{})
	for _, value := range values {
		normalized, err := NormalizeDiscoveryURL(value)
		if err == nil {
			seen[normalized] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

func isFeedMediaType(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "rss") || strings.Contains(value, "atom") || strings.Contains(value, "feed+json") || value == "application/json" || strings.Contains(value, "xml")
}

func configuredRSSHubEndpoint(crawlConfig string) string {
	var value struct {
		RSSHubURL   string `json:"rsshub_url"`
		RSSHubRoute string `json:"rsshub_route"`
	}
	if json.Unmarshal([]byte(crawlConfig), &value) != nil {
		return ""
	}
	if strings.TrimSpace(value.RSSHubURL) != "" {
		return strings.TrimSpace(value.RSSHubURL)
	}
	if strings.TrimSpace(config.RSSHUBBASEURL) != "" && strings.TrimSpace(value.RSSHubRoute) != "" {
		return strings.TrimRight(config.RSSHUBBASEURL, "/") + "/" + strings.TrimLeft(strings.TrimSpace(value.RSSHubRoute), "/")
	}
	return ""
}
