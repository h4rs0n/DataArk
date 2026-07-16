package discovery

import (
	"DataArk/config"
	"context"
	"errors"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DiscoveryPauseDepthLimit   = "depth_limit"
	DiscoveryPausePerSiteLimit = "per_site_limit"
	DiscoveryPauseDailyLimit   = "daily_limit"
	DiscoveryPauseNonBlog      = "non_blog"
	DiscoveryPauseRobots       = "robots_disallowed"
	DiscoveryMethodBlogroll    = "blogroll"
	DiscoveryMethodManualSeed  = "manual_seed"
	DiscoveryEndpointHomepage  = "homepage"
	DiscoveryPriorityBlogroll  = 1000
	DiscoveryPriorityManual    = 2000
	maxDiscoveryPriorityBoost  = 999
	defaultGraphRescanInterval = 7 * 24 * time.Hour
	failedGraphRescanInterval  = 24 * time.Hour
)

type SiteClassifier struct{}

func (SiteClassifier) Classify(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return DiscoverySiteStatusNonBlog
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	for _, domain := range []string{"facebook.com", "instagram.com", "linkedin.com", "twitter.com", "x.com", "youtube.com", "tiktok.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return DiscoverySiteStatusNonBlog
		}
	}
	path := strings.ToLower(parsed.Path)
	if strings.Contains(path, "/login") || strings.Contains(path, "/signin") || strings.Contains(path, "/cart") || strings.Contains(path, "/checkout") {
		return DiscoverySiteStatusNonBlog
	}
	return DiscoverySiteStatusObserving
}

type SiteGraphService struct {
	Clock      Clock
	Queue      JobEnqueuer
	Classifier SiteClassifier
}

type GraphApplyResult struct {
	EdgesSeen      int
	SitesCreated   int
	SitesActivated int
	TargetsPending int
	NonBlogTargets int
}

type graphWork struct {
	siteID   uint
	sourceID uint
}

func (service SiteGraphService) ApplyLinks(ctx context.Context, fromSite DiscoverySite, links []BlogrollLink) (GraphApplyResult, error) {
	if db == nil {
		return GraphApplyResult{}, nil
	}
	clock := service.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	now := clock.Now()
	maxDepth := positiveOr(config.DISCOVERYMAXGRAPHDEPTH, 3)
	perSiteLimit := positiveOr(config.DISCOVERYMAXBLOGROLLTARGETS, 50)
	dailyLimit := positiveOr(config.DISCOVERYDAILYOBSERVINGLIMIT, 100)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var activatedToday int64
	if err := db.Model(&DiscoverySite{}).
		Where("discovery_method = ? AND activated_at >= ?", DiscoveryMethodBlogroll, dayStart).
		Count(&activatedToday).Error; err != nil {
		return GraphApplyResult{}, err
	}

	unique := dedupeBlogrollLinks(links)
	result := GraphApplyResult{}
	work := make([]graphWork, 0)
	activatedThisScan := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, link := range unique {
			rootURL, hostKey, err := canonicalLegacySite(link.TargetURL)
			domainKey, domainErr := domainKeyForURL(link.TargetURL)
			if err != nil || domainErr != nil || domainKey == siteDomainKey(fromSite) {
				continue
			}
			target, created, err := findOrCreateGraphTarget(tx, rootURL, hostKey, domainKey, link, fromSite.GraphDepth+1, now, service.Classifier)
			if err != nil {
				return err
			}
			if created {
				result.SitesCreated++
			}
			pendingReason := ""
			needsActivation := created || isGraphLimitPause(target.OperationalPause)
			if target.Status == DiscoverySiteStatusNonBlog {
				pendingReason = DiscoveryPauseNonBlog
				if created {
					result.NonBlogTargets++
				}
			} else if needsActivation {
				switch {
				case fromSite.GraphDepth+1 > maxDepth:
					pendingReason = DiscoveryPauseDepthLimit
				case activatedThisScan >= perSiteLimit:
					pendingReason = DiscoveryPausePerSiteLimit
				case activatedToday >= int64(dailyLimit):
					pendingReason = DiscoveryPauseDailyLimit
				}
			}

			activate := pendingReason == "" && target.Status != DiscoverySiteStatusNonBlog && needsActivation
			if activate {
				activatedToday++
				activatedThisScan++
				result.SitesActivated++
				target.OperationalPause = ""
				target.OperationalDetails = ""
				target.CrawlAllowed = true
				target.NextGraphScanAt = &now
				target.ActivatedAt = &now
				if err := tx.Model(&target).Updates(map[string]interface{}{
					"operational_pause": "", "operational_details": "", "crawl_allowed": true,
					"next_graph_scan_at": &now, "activated_at": &now,
				}).Error; err != nil {
					return err
				}
			}
			if pendingReason != "" && pendingReason != DiscoveryPauseNonBlog {
				result.TargetsPending++
				target.OperationalPause = pendingReason
				target.OperationalDetails = "blogroll target retained for a later bounded graph pass"
				target.CrawlAllowed = false
				if err := tx.Model(&target).Updates(map[string]interface{}{
					"operational_pause":   pendingReason,
					"operational_details": target.OperationalDetails,
					"crawl_allowed":       false,
				}).Error; err != nil {
					return err
				}
			}

			edge := DiscoverySiteEdge{
				EdgeKey:    stableMigrationKey("graph-v1", fromSite.ID, target.ID, link.SourcePageURL, link.RelationType),
				FromSiteID: fromSite.ID, ToSiteID: target.ID, SourcePageURL: link.SourcePageURL,
				AnchorText: link.AnchorText, RelationType: link.RelationType, DetectionRule: link.DetectionRule,
				ContextSummary: link.ContextSummary, EvidenceSummary: compactGraphEvidence(link), Confidence: link.Confidence,
				FirstSeenAt: now, LastSeenAt: now, Active: true, GraphDepth: fromSite.GraphDepth + 1,
				PendingReason: pendingReason,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "edge_key"}},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"anchor_text": edge.AnchorText, "detection_rule": edge.DetectionRule,
					"context_summary": edge.ContextSummary, "evidence_summary": edge.EvidenceSummary,
					"confidence": edge.Confidence, "last_seen_at": now, "active": true,
					"pending_reason": pendingReason, "updated_at": now,
				}),
			}).Create(&edge).Error; err != nil {
				return err
			}
			result.EdgesSeen++
			if err := tx.Model(&target).Update("last_referenced_at", &now).Error; err != nil {
				return err
			}
			if pendingReason == "" && target.Status != DiscoverySiteStatusNonBlog {
				source, err := ensureHomepageEndpoint(tx, target, now)
				if err != nil {
					return err
				}
				if activate {
					work = append(work, graphWork{siteID: target.ID, sourceID: source.ID})
				}
			}
			if err := updateGraphPriority(tx, target.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	var enqueueErrors []error
	if service.Queue != nil {
		seen := make(map[uint]struct{})
		for _, item := range work {
			if _, exists := seen[item.siteID]; exists {
				continue
			}
			seen[item.siteID] = struct{}{}
			enqueueErrors = appendIfError(enqueueErrors, service.Queue.EnqueueFetchSource(ctx, item.sourceID))
		}
	}
	return result, errors.Join(enqueueErrors...)
}

func findOrCreateGraphTarget(tx *gorm.DB, rootURL string, hostKey string, domainKey string, link BlogrollLink, depth int, now time.Time, classifier SiteClassifier) (DiscoverySite, bool, error) {
	var target DiscoverySite
	err := tx.Where("domain_key = ? OR (domain_key = ? AND host_key = ?)", domainKey, "", hostKey).Order("id").First(&target).Error
	if err == nil {
		if depth < target.GraphDepth {
			target.GraphDepth = depth
			if err := tx.Model(&target).Update("graph_depth", depth).Error; err != nil {
				return target, false, err
			}
		}
		return target, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return target, false, err
	}
	status := classifier.Classify(rootURL)
	target = DiscoverySite{
		RootURL: rootURL, HostKey: hostKey, DomainKey: domainKey, DisplayName: firstNonBlank(link.AnchorText, hostLabel(rootURL)),
		Status: status, DiscoveryMethod: DiscoveryMethodBlogroll, GraphDepth: depth,
		CrawlAllowed: status != DiscoverySiteStatusNonBlog, RobotsStatus: "unknown",
		FirstDiscoveredAt: now, LastReferencedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if status == DiscoverySiteStatusNonBlog {
		target.OperationalPause = DiscoveryPauseNonBlog
		target.OperationalDetails = "target classified as a known non-blog platform or route"
	}
	if err := tx.Create(&target).Error; err != nil {
		return target, false, err
	}
	return target, true, nil
}

func ensureManualSeedSite(tx *gorm.DB, source *DiscoverySource, now time.Time) (DiscoverySite, error) {
	rootURL, hostKey, err := canonicalLegacySite(source.URL)
	if err != nil {
		return DiscoverySite{}, err
	}
	domainKey, err := domainKeyForURL(source.URL)
	if err != nil {
		return DiscoverySite{}, err
	}
	var site DiscoverySite
	err = tx.Where("domain_key = ? OR (domain_key = ? AND host_key = ?)", domainKey, "", hostKey).Order("id").First(&site).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		site = DiscoverySite{
			RootURL: rootURL, HostKey: hostKey, DomainKey: domainKey, DisplayName: source.Name,
			Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed,
			GraphDepth: 0, CrawlAllowed: true, RobotsStatus: "unknown",
			FirstDiscoveredAt: now, LastReferencedAt: &now, NextGraphScanAt: &now,
			CreatedAt: now, UpdatedAt: now,
		}
		return site, tx.Create(&site).Error
	}
	if err != nil {
		return site, err
	}
	updates := map[string]interface{}{
		"status": DiscoverySiteStatusSeed, "discovery_method": DiscoveryMethodManualSeed,
		"graph_depth": 0, "crawl_allowed": true, "operational_pause": "",
		"operational_details": "", "next_graph_scan_at": &now, "last_referenced_at": &now,
	}
	if source.Name != "" {
		updates["display_name"] = source.Name
	}
	if err := tx.Model(&site).Updates(updates).Error; err != nil {
		return site, err
	}
	site.Status = DiscoverySiteStatusSeed
	site.GraphDepth = 0
	site.CrawlAllowed = true
	site.OperationalPause = ""
	site.NextGraphScanAt = &now
	return site, nil
}

func ensureHomepageEndpoint(tx *gorm.DB, site DiscoverySite, now time.Time) (DiscoverySource, error) {
	var source DiscoverySource
	err := tx.Where("url = ?", site.RootURL).First(&source).Error
	if err == nil {
		updates := map[string]interface{}{"site_id": site.ID, "endpoint_type": DiscoveryEndpointHomepage, "priority": discoveryPriorityForSite(site)}
		if site.CrawlAllowed {
			updates["enabled"] = true
			if source.NextDueAt == nil {
				updates["next_due_at"] = &now
				updates["next_fetch_at"] = &now
			}
		}
		if err := tx.Model(&source).Updates(updates).Error; err != nil {
			return source, err
		}
		return source, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return source, err
	}
	source = DiscoverySource{
		Name: site.DisplayName, URL: site.RootURL, Type: DiscoverySourceTypeSite, SiteID: &site.ID,
		EndpointType: DiscoveryEndpointHomepage, Priority: discoveryPriorityForSite(site), Enabled: site.CrawlAllowed,
		NextFetchAt: &now, NextDueAt: &now,
	}
	return source, tx.Create(&source).Error
}

func updateGraphPriority(tx *gorm.DB, siteID uint) error {
	var inbound int64
	if err := tx.Model(&DiscoverySiteEdge{}).Where("to_site_id = ? AND active = ?", siteID, true).Distinct("from_site_id").Count(&inbound).Error; err != nil {
		return err
	}
	var site DiscoverySite
	if err := tx.Select("discovery_method").First(&site, siteID).Error; err != nil {
		return err
	}
	boost := int(inbound)
	if boost > maxDiscoveryPriorityBoost {
		boost = maxDiscoveryPriorityBoost
	}
	return tx.Model(&DiscoverySource{}).Where("site_id = ?", siteID).Update("priority", discoveryPriorityForSite(site)+boost).Error
}

func discoveryPriorityForSite(site DiscoverySite) int {
	if site.DiscoveryMethod == DiscoveryMethodManualSeed || site.DiscoveryMethod == "legacy_source" || site.Status == DiscoverySiteStatusSeed {
		return DiscoveryPriorityManual
	}
	return DiscoveryPriorityBlogroll
}

func dedupeBlogrollLinks(links []BlogrollLink) []BlogrollLink {
	byHost := make(map[string]BlogrollLink)
	for _, link := range links {
		domainKey, err := domainKeyForURL(link.TargetURL)
		if err != nil {
			continue
		}
		existing, found := byHost[domainKey]
		if !found || link.Confidence > existing.Confidence {
			byHost[domainKey] = link
		}
	}
	result := make([]BlogrollLink, 0, len(byHost))
	for _, link := range byHost {
		result = append(result, link)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Confidence == result[j].Confidence {
			return result[i].TargetURL < result[j].TargetURL
		}
		return result[i].Confidence > result[j].Confidence
	})
	return result
}

func compactGraphEvidence(link BlogrollLink) string {
	evidence := strings.TrimSpace(strings.Join([]string{link.DetectionRule, link.AnchorText}, ": "))
	if len(evidence) > 240 {
		evidence = evidence[:240]
	}
	return evidence
}

func positiveOr(value int, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isGraphLimitPause(value string) bool {
	switch value {
	case DiscoveryPauseDepthLimit, DiscoveryPausePerSiteLimit, DiscoveryPauseDailyLimit:
		return true
	default:
		return false
	}
}

func appendIfError(values []error, err error) []error {
	if err != nil {
		return append(values, err)
	}
	return values
}
