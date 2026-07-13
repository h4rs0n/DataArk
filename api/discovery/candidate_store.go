package discovery

import (
	"DataArk/archive"
	"DataArk/jobqueue"
	"context"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var enqueueCandidateForProcessing = func(ctx context.Context, candidate DiscoveryCandidate) error {
	queue, available := jobqueue.Default()
	if !available {
		return nil
	}
	return queue.EnqueueProcessCandidate(ctx, candidate.ID, candidateContentVersion(candidate))
}

const (
	DiscoveryProcessingFetchPending = "fetch_pending"
	DiscoveryMethodFeed             = "feed"
	DiscoveryMethodSitemap          = "sitemap"
	DiscoveryMethodHomepageLink     = "homepage_link"
)

type candidateWriteResult struct {
	Candidate DiscoveryCandidate
	Created   bool
	Updated   bool
}

func upsertDiscoveryCandidate(source DiscoverySource, candidate discoveredCandidate) (candidateWriteResult, error) {
	result := candidateWriteResult{}
	normalizedURL, err := NormalizeDiscoveryURL(candidate.URL)
	if err != nil {
		return result, nil
	}
	articleURL, err := NormalizeArticleURL(normalizedURL)
	if err != nil {
		articleURL = normalizedURL
	}
	now := discoveryClock.Now()
	method := strings.TrimSpace(candidate.DiscoveryMethod)
	if method == "" {
		method = discoveryMethodForSource(source)
	}
	confidence := candidate.MetadataConfidence
	if confidence <= 0 {
		confidence = metadataConfidenceForMethod(method)
	}
	if db == nil {
		return result, nil
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		var record DiscoveryCandidate
		findErr := tx.Where("url = ? OR normalized_url = ?", normalizedURL, articleURL).Order("id").First(&record).Error
		switch {
		case errors.Is(findErr, gorm.ErrRecordNotFound):
			record = DiscoveryCandidate{
				SourceID: source.ID, SourceName: source.Name, URL: normalizedURL,
				NormalizedURL: articleURL, CanonicalURL: articleURL,
				Title: strings.TrimSpace(candidate.Title), Summary: archive.BuildSummary(candidate.Summary, 260),
				Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingFetchPending,
				EligibilityState:   DiscoveryEligibilityUnknown,
				EnrichmentStatus:   DiscoveryCandidateEnrichmentStatusPending,
				MetadataConfidence: confidence, DedupeKey: articleURL,
				Score: scoreDiscoveredCandidate(candidate), PublishedAt: candidate.PublishedAt,
				LastSeenAt: now, FirstSeenAt: &now, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			result.Created = true
		case findErr != nil:
			return findErr
		default:
			updates := map[string]interface{}{"last_seen_at": now, "updated_at": now}
			incomingTitle := strings.TrimSpace(candidate.Title)
			incomingSummary := archive.BuildSummary(candidate.Summary, 260)
			trusted := confidence >= record.MetadataConfidence
			if incomingTitle != "" && (record.Title == "" || trusted) {
				updates["title"] = incomingTitle
				record.Title = incomingTitle
			}
			if incomingSummary != "" && (record.Summary == "" || trusted) {
				updates["summary"] = incomingSummary
				record.Summary = incomingSummary
			}
			if candidate.PublishedAt != nil && (record.PublishedAt == nil || trusted) {
				updates["published_at"] = candidate.PublishedAt
				record.PublishedAt = candidate.PublishedAt
			}
			if confidence > record.MetadataConfidence {
				updates["metadata_confidence"] = confidence
				record.MetadataConfidence = confidence
			}
			if record.NormalizedURL == "" {
				updates["normalized_url"] = articleURL
				record.NormalizedURL = articleURL
			}
			if record.CanonicalURL == "" {
				updates["canonical_url"] = articleURL
				record.CanonicalURL = articleURL
			}
			if record.DedupeKey == "" {
				updates["dedupe_key"] = articleURL
				record.DedupeKey = articleURL
			}
			if err := tx.Model(&record).Updates(updates).Error; err != nil {
				return err
			}
			result.Updated = len(updates) > 2
		}
		record.LastSeenAt = now
		if source.SiteID != nil {
			sourceID := source.ID
			provenance := DiscoveryCandidateProvenance{
				ProvenanceKey: stableMigrationKey("discovery-v1", record.ID, *source.SiteID, source.ID, method, normalizedURL, candidate.SourcePageURL),
				CandidateID:   record.ID, SiteID: *source.SiteID, SourceID: &sourceID,
				DiscoveryMethod: method, OriginalURL: normalizedURL,
				SourcePageURL: candidate.SourcePageURL, FirstSeenAt: now, LastSeenAt: now,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "provenance_key"}},
				DoUpdates: clause.Assignments(map[string]interface{}{"last_seen_at": now, "updated_at": now}),
			}).Create(&provenance).Error; err != nil {
				return err
			}
		}
		result.Candidate = record
		return nil
	})
	return result, err
}

func discoveryMethodForSource(source DiscoverySource) string {
	switch source.EndpointType {
	case "sitemap":
		return DiscoveryMethodSitemap
	case DiscoveryEndpointHomepage:
		return DiscoveryMethodHomepageLink
	default:
		return DiscoveryMethodFeed
	}
}

func metadataConfidenceForMethod(method string) int {
	switch method {
	case DiscoveryMethodFeed:
		return 80
	case DiscoveryMethodHomepageLink:
		return 40
	case DiscoveryMethodSitemap:
		return 20
	default:
		return 10
	}
}

func candidateContentVersion(candidate DiscoveryCandidate) string {
	return strconv.FormatUint(uint64(candidate.ContentVersion), 10)
}
