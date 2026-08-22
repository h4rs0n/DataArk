package discovery

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DiscoveryEndpointOperations struct {
	Source   DiscoverySource                  `json:"source"`
	Decision *DiscoverySourceScheduleDecision `json:"scheduleDecision,omitempty"`
}

type DiscoverySiteOperations struct {
	Site      DiscoverySite                 `json:"site"`
	Stats     DiscoverySiteOperationalStats `json:"stats"`
	Endpoints []DiscoveryEndpointOperations `json:"endpoints"`
	Backfills []BackfillCoverage            `json:"backfills"`
}

func RefreshDiscoverySiteOperationalStats(siteID uint) (*DiscoverySiteOperationalStats, error) {
	if db == nil || siteID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var site DiscoverySite
	if err := db.First(&site, siteID).Error; err != nil {
		return nil, err
	}
	now := discoveryClock.Now()
	stats := DiscoverySiteOperationalStats{SiteID: siteID, LastComputedAt: now, CreatedAt: now, UpdatedAt: now}
	var inbound int64
	if err := db.Model(&DiscoverySiteEdge{}).Where("to_site_id = ? AND active = ?", siteID, true).Distinct("from_site_id").Count(&inbound).Error; err != nil {
		return nil, err
	}
	stats.IndependentInboundSites = uint(inbound)
	if err := countSiteFetchRuns(siteID, &stats); err != nil {
		return nil, err
	}
	if err := countSiteCandidates(siteID, &stats); err != nil {
		return nil, err
	}
	var coverage struct {
		URLsSeen      uint
		ArticlesFound uint
	}
	if err := db.Model(&DiscoveryBackfillState{}).Select("COALESCE(SUM(urls_seen), 0) AS urls_seen, COALESCE(SUM(articles_found), 0) AS articles_found").Where("site_id = ?", siteID).Scan(&coverage).Error; err != nil {
		return nil, err
	}
	stats.BackfillURLsSeen = coverage.URLsSeen
	stats.BackfillArticlesFound = coverage.ArticlesFound
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "site_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"independent_inbound_sites", "fetch_attempts", "fetch_successes", "not_modified_fetches", "parse_successes",
			"candidate_count", "eligible_candidate_count", "duplicate_candidate_count", "extracted_candidate_count",
			"positive_feedback_articles", "backfill_urls_seen", "backfill_articles_found", "last_computed_at", "updated_at",
		}),
	}).Create(&stats).Error; err != nil {
		return nil, err
	}
	if err := db.Where("site_id = ?", siteID).First(&stats).Error; err != nil {
		return nil, err
	}
	return &stats, nil
}

func countSiteFetchRuns(siteID uint, stats *DiscoverySiteOperationalStats) error {
	queries := []struct {
		target *uint
		where  string
		args   []interface{}
	}{
		{&stats.FetchAttempts, "site_id = ?", []interface{}{siteID}},
		{&stats.FetchSuccesses, "site_id = ? AND status = ?", []interface{}{siteID, "succeeded"}},
		{&stats.NotModifiedFetches, "site_id = ? AND not_modified = ?", []interface{}{siteID, true}},
		{&stats.ParseSuccesses, "site_id = ? AND status = ?", []interface{}{siteID, "succeeded"}},
	}
	for _, query := range queries {
		var count int64
		if err := db.Model(&DiscoveryFetchRun{}).Where(query.where, query.args...).Count(&count).Error; err != nil {
			return err
		}
		*query.target = uint(count)
	}
	return nil
}

func countSiteCandidates(siteID uint, stats *DiscoverySiteOperationalStats) error {
	base := func() *gorm.DB {
		return db.Table("discovery_candidates AS candidates").Joins("JOIN discovery_candidate_provenances AS provenance ON provenance.candidate_id = candidates.id").Where("provenance.site_id = ?", siteID)
	}
	queries := []struct {
		target *uint
		where  string
		args   []interface{}
	}{
		{&stats.CandidateCount, "", nil},
		{&stats.EligibleCandidateCount, "candidates.processing_state = ? AND candidates.eligibility_state = ? AND candidates.dedupe_state = ? AND (candidates.representative_id IS NULL OR candidates.representative_id = candidates.id)", []interface{}{DiscoveryProcessingReady, DiscoveryEligibilityEligible, DiscoveryDedupeReady}},
		{&stats.DuplicateCandidateCount, "candidates.representative_id IS NOT NULL AND candidates.representative_id <> candidates.id", nil},
		{&stats.ExtractedCandidateCount, "candidates.processing_state = ?", []interface{}{DiscoveryProcessingReady}},
	}
	for _, query := range queries {
		var count int64
		current := base()
		if query.where != "" {
			current = current.Where(query.where, query.args...)
		}
		if err := current.Distinct("candidates.id").Count(&count).Error; err != nil {
			return err
		}
		*query.target = uint(count)
	}
	var positive int64
	if err := base().Joins("JOIN user_candidate_states AS user_state ON user_state.candidate_id = candidates.id").Where("user_state.current_feedback IN ?", []string{UserCandidateFeedbackValuable, UserCandidateFeedbackDeepRead}).Distinct("candidates.id").Count(&positive).Error; err != nil {
		return err
	}
	stats.PositiveFeedbackArticles = uint(positive)
	return nil
}

func RefreshCandidateSiteOperationalStats(candidateID uint) error {
	if db == nil || candidateID == 0 {
		return nil
	}
	var siteIDs []uint
	if err := db.Model(&DiscoveryCandidateProvenance{}).Where("candidate_id = ?", candidateID).Distinct("site_id").Pluck("site_id", &siteIDs).Error; err != nil {
		return err
	}
	for _, siteID := range siteIDs {
		if _, err := RefreshDiscoverySiteOperationalStats(siteID); err != nil {
			return err
		}
	}
	return nil
}

func loadDiscoverySiteOperationalStats(siteID *uint) DiscoverySiteOperationalStats {
	if db == nil || siteID == nil {
		return DiscoverySiteOperationalStats{}
	}
	var stats DiscoverySiteOperationalStats
	if err := db.Where("site_id = ?", *siteID).First(&stats).Error; err != nil {
		return DiscoverySiteOperationalStats{}
	}
	return stats
}

func SaveDiscoverySourceScheduleDecision(source DiscoverySource, decision SourceScheduleDecision) error {
	if db == nil || source.ID == 0 || decision.NextDueAt.IsZero() {
		return nil
	}
	now := discoveryClock.Now()
	record := DiscoverySourceScheduleDecision{
		SourceID: source.ID, SiteID: source.SiteID, Basis: decision.Basis,
		BaseIntervalSeconds: int64(decision.Base / time.Second), ChosenIntervalSeconds: int64(decision.Chosen / time.Second),
		Explanation: decision.Explanation, NextDueAt: decision.NextDueAt, ComputedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"site_id", "basis", "base_interval_seconds", "chosen_interval_seconds", "explanation", "next_due_at", "computed_at", "updated_at"}),
	}).Create(&record).Error
}

func GetDiscoverySiteOperations(siteID uint) (*DiscoverySiteOperations, error) {
	stats, err := RefreshDiscoverySiteOperationalStats(siteID)
	if err != nil {
		return nil, err
	}
	var site DiscoverySite
	if err := db.First(&site, siteID).Error; err != nil {
		return nil, err
	}
	var sources []DiscoverySource
	if err := db.Where("site_id = ?", siteID).Order("id").Find(&sources).Error; err != nil {
		return nil, err
	}
	endpoints := make([]DiscoveryEndpointOperations, 0, len(sources))
	for _, source := range sources {
		item := DiscoveryEndpointOperations{Source: source}
		var decision DiscoverySourceScheduleDecision
		if err := db.Where("source_id = ?", source.ID).First(&decision).Error; err == nil {
			item.Decision = &decision
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		endpoints = append(endpoints, item)
	}
	backfills, err := ListBackfillCoverage(siteID)
	if err != nil {
		return nil, err
	}
	return &DiscoverySiteOperations{Site: site, Stats: *stats, Endpoints: endpoints, Backfills: backfills}, nil
}
