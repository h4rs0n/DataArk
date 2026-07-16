package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BackfillV3Compatibility maps legacy sources and candidates into the v3
// additive tables. It is safe to run after every application restart.
func BackfillV3Compatibility(database *gorm.DB) error {
	if database == nil {
		return nil
	}
	return database.Transaction(func(tx *gorm.DB) error {
		if err := backfillDiscoverySiteDomainKeys(tx); err != nil {
			return err
		}
		var sources []DiscoverySource
		if err := tx.Order("id").Find(&sources).Error; err != nil {
			return err
		}
		siteBySource := make(map[uint]uint, len(sources))
		for index := range sources {
			source := &sources[index]
			wasLegacySource := source.SiteID == nil
			rootURL, hostKey, err := canonicalLegacySite(source.URL)
			if err != nil {
				return fmt.Errorf("source %d site identity: %w", source.ID, err)
			}
			domainKey, err := domainKeyForURL(source.URL)
			if err != nil {
				return fmt.Errorf("source %d domain identity: %w", source.ID, err)
			}
			firstSeen := firstNonZeroTime(source.CreatedAt, source.UpdatedAt, time.Now())
			site := DiscoverySite{
				RootURL:           rootURL,
				HostKey:           hostKey,
				DomainKey:         domainKey,
				DisplayName:       source.Name,
				Status:            DiscoverySiteStatusSeed,
				DiscoveryMethod:   "legacy_source",
				GraphDepth:        0,
				CrawlAllowed:      true,
				RobotsStatus:      "unknown",
				FirstDiscoveredAt: firstSeen,
				LastReferencedAt:  timePointer(firstNonZeroTime(source.UpdatedAt, firstSeen)),
			}
			if err := tx.Where("domain_key = ?", domainKey).Order("id").First(&site).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				site = DiscoverySite{
					RootURL: rootURL, HostKey: hostKey, DomainKey: domainKey, DisplayName: source.Name,
					Status: DiscoverySiteStatusSeed, DiscoveryMethod: "legacy_source", GraphDepth: 0,
					CrawlAllowed: true, RobotsStatus: "unknown", FirstDiscoveredAt: firstSeen,
					LastReferencedAt: timePointer(firstNonZeroTime(source.UpdatedAt, firstSeen)),
				}
				if createErr := tx.Create(&site).Error; createErr != nil {
					return createErr
				}
			} else if err != nil {
				return err
			}
			siteBySource[source.ID] = site.ID
			updates := map[string]interface{}{
				"site_id":       site.ID,
				"endpoint_type": legacyEndpointType(source.Type),
				"priority":      discoveryPriorityForSite(site),
			}
			if wasLegacySource {
				updates["user_managed"] = true
			}
			if source.NextDueAt == nil && source.NextFetchAt != nil {
				updates["next_due_at"] = source.NextFetchAt
			}
			if err := tx.Model(&DiscoverySource{}).Where("id = ?", source.ID).Updates(updates).Error; err != nil {
				return err
			}
		}

		var candidates []DiscoveryCandidate
		if err := tx.Order("id").Find(&candidates).Error; err != nil {
			return err
		}
		for index := range candidates {
			candidate := &candidates[index]
			siteID, ok := siteBySource[candidate.SourceID]
			if !ok {
				continue
			}
			firstSeen := firstNonZeroTime(candidate.CreatedAt, candidate.LastSeenAt, time.Now())
			lastSeen := firstNonZeroTime(candidate.LastSeenAt, candidate.UpdatedAt, firstSeen)
			updates := map[string]interface{}{}
			if candidate.FirstSeenAt == nil {
				updates["first_seen_at"] = firstSeen
			}
			if state := strings.TrimSpace(candidate.ProcessingState); state == "" || state == DiscoveryProcessingDiscovered {
				updates["processing_state"] = DiscoveryProcessingFetchPending
			}
			if strings.TrimSpace(candidate.EligibilityState) == "" {
				updates["eligibility_state"] = DiscoveryEligibilityUnknown
			}
			if len(updates) > 0 {
				if err := tx.Model(&DiscoveryCandidate{}).Where("id = ?", candidate.ID).Updates(updates).Error; err != nil {
					return err
				}
			}
			sourceID := candidate.SourceID
			provenance := DiscoveryCandidateProvenance{
				ProvenanceKey:   stableMigrationKey("legacy", candidate.ID, siteID, sourceID, candidate.URL),
				CandidateID:     candidate.ID,
				SiteID:          siteID,
				SourceID:        &sourceID,
				DiscoveryMethod: "legacy_source",
				OriginalURL:     candidate.URL,
				FirstSeenAt:     firstSeen,
				LastSeenAt:      lastSeen,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "provenance_key"}},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"last_seen_at": lastSeen,
					"updated_at":   time.Now(),
				}),
			}).Create(&provenance).Error; err != nil {
				return err
			}
			if candidate.Status == DiscoveryCandidateStatusRead || candidate.Status == DiscoveryCandidateStatusIgnored || candidate.Status == DiscoveryCandidateStatusArchived {
				review := DiscoveryLegacyCandidateStateReview{
					CandidateID: candidate.ID, LegacyStatus: candidate.Status, Resolution: "pending",
					Notes:     "legacy global state has no reliable user identity; retained for owner review",
					CreatedAt: firstSeen, UpdatedAt: lastSeen,
				}
				if err := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{{Name: "candidate_id"}},
					DoUpdates: clause.Assignments(map[string]interface{}{
						"legacy_status": candidate.Status, "updated_at": lastSeen,
					}),
				}).Create(&review).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func backfillDiscoverySiteDomainKeys(tx *gorm.DB) error {
	var sites []DiscoverySite
	if err := tx.Order("id").Find(&sites).Error; err != nil {
		return err
	}
	for _, site := range sites {
		domainKey, err := domainKeyForURL(site.RootURL)
		if err != nil {
			return fmt.Errorf("site %d domain identity: %w", site.ID, err)
		}
		if site.DomainKey != domainKey {
			if err := tx.Model(&DiscoverySite{}).Where("id = ?", site.ID).Update("domain_key", domainKey).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func canonicalLegacySite(rawURL string) (string, string, error) {
	parsedURL, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", "", errors.New("unsupported site scheme")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsedURL.Hostname(), "."))
	if hostname == "" {
		return "", "", errors.New("missing site host")
	}
	hostKey := strings.TrimPrefix(hostname, "www.")
	port := parsedURL.Port()
	if port != "" && !((parsedURL.Scheme == "http" && port == "80") || (parsedURL.Scheme == "https" && port == "443")) {
		hostKey += ":" + port
	}
	rootHost := hostname
	if port != "" {
		rootHost += ":" + port
	}
	return strings.ToLower(parsedURL.Scheme) + "://" + rootHost + "/", hostKey, nil
}

func legacyEndpointType(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case DiscoverySourceTypeFeed:
		return "feed"
	case DiscoverySourceTypeRSSHub:
		return "rsshub"
	case DiscoverySourceTypeSite:
		return "homepage"
	case DiscoverySourceTypeSitemap:
		return DiscoveryEndpointSitemap
	default:
		return "legacy"
	}
}

func stableMigrationKey(parts ...interface{}) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = fmt.Fprintf(hash, "%v\x00", part)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
