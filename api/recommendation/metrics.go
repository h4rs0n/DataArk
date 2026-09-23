package recommendation

import (
	"DataArk/assessment"
	"DataArk/discovery"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
)

type ProductMetricRate struct {
	Numerator   int64   `json:"numerator"`
	Denominator int64   `json:"denominator"`
	Rate        float64 `json:"rate"`
}

type AdminProductMetrics struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Sites       struct {
		ByStatus                map[string]int64 `json:"byStatus"`
		ByDepth                 map[int]int64    `json:"byDepth"`
		GraphEdges              int64            `json:"graphEdges"`
		ScheduleFloorViolations int64            `json:"scheduleFloorViolations"`
	} `json:"sites"`
	Fetch struct {
		Attempts     int64 `json:"attempts"`
		Successes    int64 `json:"successes"`
		NotModified  int64 `json:"notModified"`
		Failures     int64 `json:"failures"`
		RobotsDenied int64 `json:"robotsDenied"`
	} `json:"fetch"`
	Candidates struct {
		Total            int64 `json:"total"`
		Ready            int64 `json:"ready"`
		Eligible         int64 `json:"eligible"`
		Review           int64 `json:"review"`
		Failed           int64 `json:"failed"`
		Duplicate        int64 `json:"duplicate"`
		BackfillEligible int64 `json:"backfillEligible"`
	} `json:"candidates"`
	Assessments struct {
		Rules          int64             `json:"rules"`
		Model          int64             `json:"model"`
		ModelUsageRate ProductMetricRate `json:"modelUsageRate"`
	} `json:"assessments"`
	Digests struct {
		Published            int64             `json:"published"`
		Failed               int64             `json:"failed"`
		FillRate             ProductMetricRate `json:"fillRate"`
		DegradationRate      ProductMetricRate `json:"degradationRate"`
		OnTimePublishRate    ProductMetricRate `json:"onTimePublishRate"`
		ExplorationQuotaRate ProductMetricRate `json:"explorationQuotaRate"`
		SourceConcentration  float64           `json:"sourceConcentration"`
		TopicConcentration   float64           `json:"topicConcentration"`
		AuthorConcentration  float64           `json:"authorConcentration"`
		ShortageReasons      map[string]int64  `json:"shortageReasons"`
	} `json:"digests"`
	Feedback struct {
		ByAction              map[string]int64             `json:"byAction"`
		ActionRates           map[string]ProductMetricRate `json:"actionRates"`
		PositiveArticles      int64                        `json:"positiveArticles"`
		ExplorationAcceptance ProductMetricRate            `json:"explorationAcceptance"`
	} `json:"feedback"`
	Integrity struct {
		HardFilterViolations       int64 `json:"hardFilterViolations"`
		DuplicateClusterViolations int64 `json:"duplicateClusterViolations"`
		ActiveBlockViolations      int64 `json:"activeBlockViolations"`
	} `json:"integrity"`
	LongTail struct {
		GemContribution          ProductMetricRate `json:"gemContribution"`
		ContributingSites        int64             `json:"contributingSites"`
		BlogrollPositiveArticles int64             `json:"blogrollPositiveArticles"`
		BackfillPositiveArticles int64             `json:"backfillPositiveArticles"`
		AverageHoursToFirstGem   float64           `json:"averageHoursToFirstGem"`
	} `json:"longTail"`
}

// GetAdminProductMetrics 返回产品运营指标（owner）。
func GetAdminProductMetrics(now time.Time) (*AdminProductMetrics, error) {
	metrics := &AdminProductMetrics{GeneratedAt: now}
	metrics.Sites.ByStatus, metrics.Digests.ShortageReasons, metrics.Feedback.ByAction = map[string]int64{}, map[string]int64{}, map[string]int64{}
	metrics.Feedback.ActionRates = map[string]ProductMetricRate{}
	metrics.Sites.ByDepth = map[int]int64{}
	if db == nil {
		return metrics, nil
	}
	var sites []discovery.DiscoverySite
	if err := db.Find(&sites).Error; err != nil {
		return nil, err
	}
	for _, site := range sites {
		metrics.Sites.ByStatus[site.Status]++
		metrics.Sites.ByDepth[site.GraphDepth]++
	}
	db.Model(&discovery.DiscoverySiteEdge{}).Where("active = ?", true).Count(&metrics.Sites.GraphEdges)
	var decisions []discovery.DiscoverySourceScheduleDecision
	if err := db.Find(&decisions).Error; err != nil {
		return nil, err
	}
	for _, decision := range decisions {
		if decision.BaseIntervalSeconds > 0 && decision.ChosenIntervalSeconds > decision.BaseIntervalSeconds {
			metrics.Sites.ScheduleFloorViolations++
		}
	}
	var runs []discovery.DiscoveryFetchRun
	if err := db.Find(&runs).Error; err != nil {
		return nil, err
	}
	for _, run := range runs {
		metrics.Fetch.Attempts++
		if run.Status == "succeeded" {
			metrics.Fetch.Successes++
		}
		if run.NotModified {
			metrics.Fetch.NotModified++
		}
		if run.Status == "failed" {
			metrics.Fetch.Failures++
		}
		if run.RobotsStatus == "denied" || run.ErrorCategory == "robots" {
			metrics.Fetch.RobotsDenied++
		}
	}
	var candidates []DiscoveryCandidate
	if err := db.Find(&candidates).Error; err != nil {
		return nil, err
	}
	candidateByID := make(map[uint]DiscoveryCandidate, len(candidates))
	for _, candidate := range candidates {
		candidateByID[candidate.ID] = candidate
		metrics.Candidates.Total++
		if candidate.ProcessingState == discovery.DiscoveryProcessingReady {
			metrics.Candidates.Ready++
		}
		if candidate.EligibilityState == discovery.DiscoveryEligibilityEligible {
			metrics.Candidates.Eligible++
		}
		if candidate.EligibilityState == discovery.DiscoveryEligibilityReview {
			metrics.Candidates.Review++
		}
		if candidate.ProcessingState == discovery.DiscoveryProcessingFailed {
			metrics.Candidates.Failed++
		}
		if candidate.RepresentativeID != nil && *candidate.RepresentativeID != candidate.ID {
			metrics.Candidates.Duplicate++
		}
	}
	var rows []assessment.ArticleAssessment
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Assessor), "rule") {
			metrics.Assessments.Rules++
		} else {
			metrics.Assessments.Model++
		}
	}
	metrics.Assessments.ModelUsageRate = metricRate(metrics.Assessments.Model, int64(len(rows)))
	var days []RecommendationDay
	if err := db.Find(&days).Error; err != nil {
		return nil, err
	}
	settingsByUser := map[uint]RecommendationSettings{}
	var settings []RecommendationSettings
	_ = db.Find(&settings).Error
	for _, value := range settings {
		settingsByUser[value.UserID] = value
	}
	var totalRequested, totalActual, completedExploration, eligibleExplorationDays, onTimePublished int64
	for _, day := range days {
		if day.Status == RecommendationDayStatusPublished || day.Status == RecommendationDayStatusSupplemented {
			metrics.Digests.Published++
			totalRequested += int64(day.RequestedCount)
			totalActual += int64(day.ActualCount)
			if day.Degraded {
				metrics.Digests.DegradationRate.Numerator++
			}
			if day.GeneratedAt != nil && digestPublishedOnTime(day, settingsByUser[day.UserID]) {
				onTimePublished++
			}
			var exploration int64
			db.Model(&RecommendationItem{}).Where("day_id = ? AND pool_type = ?", day.ID, "exploration").Count(&exploration)
			rate := settingsByUser[day.UserID].ExplorationRate
			target := int64(math.Ceil(float64(day.RequestedCount) * rate))
			if rate > 0 && day.RequestedCount >= 5 && target == 0 {
				target = 1
			}
			if target > 0 {
				eligibleExplorationDays++
				if exploration >= target {
					completedExploration++
				}
			}
			collectShortageReasons(metrics.Digests.ShortageReasons, day.ShortageReasons)
		} else if day.Status == RecommendationDayStatusFailed {
			metrics.Digests.Failed++
		}
	}
	metrics.Digests.FillRate = metricRate(totalActual, totalRequested)
	metrics.Digests.DegradationRate.Denominator = metrics.Digests.Published
	metrics.Digests.DegradationRate.Rate = safeRate(metrics.Digests.DegradationRate.Numerator, metrics.Digests.Published)
	metrics.Digests.ExplorationQuotaRate = metricRate(completedExploration, eligibleExplorationDays)
	metrics.Digests.OnTimePublishRate = metricRate(onTimePublished, metrics.Digests.Published)
	var feedback []RecommendationFeedback
	if err := db.Find(&feedback).Error; err != nil {
		return nil, err
	}
	positiveItems := map[uint]uint{}
	for _, event := range feedback {
		metrics.Feedback.ByAction[event.Action]++
		if event.Action == RecommendationFeedbackValuable || event.Action == RecommendationFeedbackDeepRead {
			positiveItems[event.RecommendationItemID] = event.CandidateID
		}
	}
	for action, count := range metrics.Feedback.ByAction {
		metrics.Feedback.ActionRates[action] = metricRate(count, int64(len(feedback)))
	}
	var states []discovery.UserCandidateState
	_ = db.Where("archived_at IS NOT NULL").Find(&states).Error
	for _, state := range states {
		var item RecommendationItem
		if db.Where("user_id = ? AND candidate_id = ?", state.UserID, state.CandidateID).Order("id desc").Limit(1).Find(&item).RowsAffected > 0 {
			positiveItems[item.ID] = item.CandidateID
		}
	}
	metrics.Feedback.PositiveArticles = int64(len(positiveItems))
	computeConcentrationAndExploration(metrics, positiveItems)
	computeIntegrityMetrics(metrics, candidateByID)
	if err := computeLongTailMetrics(metrics, positiveItems); err != nil {
		return nil, err
	}
	return metrics, nil
}

func digestPublishedOnTime(day RecommendationDay, settings RecommendationSettings) bool {
	if day.GeneratedAt == nil {
		return false
	}
	location := recommendationLocation(RecommendationSettings{UserID: day.UserID, Timezone: day.Timezone})
	localDate, err := time.ParseInLocation("2006-01-02", day.RecommendationDate, location)
	if err != nil {
		return false
	}
	hour, minute, ok := parseRecommendationGenerationTime(settings.GenerationTime)
	if !ok {
		hour, minute = 7, 0
	}
	due := time.Date(localDate.Year(), localDate.Month(), localDate.Day(), hour, minute, 0, 0, location)
	return !day.GeneratedAt.After(due.Add(15 * time.Minute))
}

func computeConcentrationAndExploration(metrics *AdminProductMetrics, positiveItems map[uint]uint) {
	var items []RecommendationItem
	db.Table("recommendation_items AS item").Select("item.*").Joins("JOIN recommendation_days AS day ON day.id = item.day_id").Where("day.status IN ?", []string{RecommendationDayStatusPublished, RecommendationDayStatusSupplemented, "generated"}).Scan(&items)
	sources, topics, authors := map[string]int64{}, map[string]int64{}, map[string]int64{}
	var exploration, positiveExploration int64
	for _, item := range items {
		if value := strings.ToLower(strings.TrimSpace(item.SnapshotSource)); value != "" {
			sources[value]++
		}
		if value := strings.ToLower(strings.TrimSpace(item.SnapshotAuthor)); value != "" {
			authors[value]++
		}
		for _, topic := range parseStringList(item.SnapshotTopics) {
			topics[strings.ToLower(topic)]++
		}
		if item.PoolType == "exploration" {
			exploration++
			if _, ok := positiveItems[item.ID]; ok {
				positiveExploration++
			}
		}
	}
	metrics.Digests.SourceConcentration = maximumShare(sources, int64(len(items)))
	metrics.Digests.TopicConcentration = maximumShare(topics, int64(len(items)))
	metrics.Digests.AuthorConcentration = maximumShare(authors, int64(len(items)))
	metrics.Feedback.ExplorationAcceptance = metricRate(positiveExploration, exploration)
}

func maximumShare(values map[string]int64, denominator int64) float64 {
	var maximum int64
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return safeRate(maximum, denominator)
}

func metricRate(numerator, denominator int64) ProductMetricRate {
	return ProductMetricRate{Numerator: numerator, Denominator: denominator, Rate: safeRate(numerator, denominator)}
}
func safeRate(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func collectShortageReasons(target map[string]int64, raw string) {
	var audit struct {
		Excluded map[string]int `json:"excluded"`
	}
	if json.Unmarshal([]byte(raw), &audit) != nil {
		return
	}
	for key, count := range audit.Excluded {
		target[key] += int64(count)
	}
}

func computeIntegrityMetrics(metrics *AdminProductMetrics, candidates map[uint]DiscoveryCandidate) {
	var items []RecommendationItem
	_ = db.Table("recommendation_items AS item").Select("item.*").Joins("JOIN recommendation_days AS day ON day.id = item.day_id").Where("day.status IN ?", []string{RecommendationDayStatusPublished, RecommendationDayStatusSupplemented, "generated"}).Scan(&items).Error
	var rules []UserBlockRule
	_ = db.Where("active = ?", true).Find(&rules).Error
	rulesByUser := map[uint][]UserBlockRule{}
	for _, rule := range rules {
		rulesByUser[rule.UserID] = append(rulesByUser[rule.UserID], rule)
	}
	clustersByDay := map[uint]map[string]bool{}
	for _, item := range items {
		candidate, ok := candidates[item.CandidateID]
		if !ok {
			metrics.Integrity.HardFilterViolations++
			continue
		}
		processingState := firstNonEmpty(item.SnapshotProcessingState, candidate.ProcessingState)
		eligibilityState := firstNonEmpty(item.SnapshotEligibilityState, candidate.EligibilityState)
		dedupeState := firstNonEmpty(item.SnapshotDedupeState, candidate.DedupeState)
		if processingState != discovery.DiscoveryProcessingReady || eligibilityState != discovery.DiscoveryEligibilityEligible || dedupeState != discovery.DiscoveryDedupeReady {
			metrics.Integrity.HardFilterViolations++
		}
		applicableRules := make([]UserBlockRule, 0, len(rulesByUser[item.UserID]))
		for _, rule := range rulesByUser[item.UserID] {
			if rule.CreatedAt.IsZero() || !rule.CreatedAt.After(item.CreatedAt) {
				applicableRules = append(applicableRules, rule)
			}
		}
		snapshotCandidate := candidate
		snapshotCandidate.SourceName = firstNonEmpty(item.SnapshotSource, candidate.SourceName)
		snapshotCandidate.Topics = firstNonEmpty(item.SnapshotTopics, candidate.Topics)
		snapshotCandidate.ContentStyle = firstNonEmpty(item.SnapshotStyle, candidate.ContentStyle)
		snapshotCandidate.ContentType = firstNonEmpty(item.SnapshotContentType, candidate.ContentType)
		snapshotCandidate.URL = firstNonEmpty(item.SnapshotURL, candidate.URL)
		if candidateBlocked(snapshotCandidate, parseStringList(snapshotCandidate.Topics), sourceHost(snapshotCandidate.URL), applicableRules) {
			metrics.Integrity.ActiveBlockViolations++
		}
		cluster := strings.TrimSpace(firstNonEmpty(item.SnapshotClusterID, candidate.DuplicateClusterID))
		if cluster != "" && item.DayID != nil {
			dayID := *item.DayID
			if clustersByDay[dayID] == nil {
				clustersByDay[dayID] = map[string]bool{}
			}
			if clustersByDay[dayID][cluster] {
				metrics.Integrity.DuplicateClusterViolations++
			}
			clustersByDay[dayID][cluster] = true
		}
	}
}

func computeLongTailMetrics(metrics *AdminProductMetrics, positiveItems map[uint]uint) error {
	var provenance []discovery.DiscoveryCandidateProvenance
	if err := db.Find(&provenance).Error; err != nil {
		return err
	}
	byCandidate := map[uint][]discovery.DiscoveryCandidateProvenance{}
	siteCandidate := map[uint]map[uint]bool{}
	for _, row := range provenance {
		byCandidate[row.CandidateID] = append(byCandidate[row.CandidateID], row)
		if siteCandidate[row.SiteID] == nil {
			siteCandidate[row.SiteID] = map[uint]bool{}
		}
		siteCandidate[row.SiteID][row.CandidateID] = true
	}
	var eligible []uint
	discovery.Candidates(db).Model(&DiscoveryCandidate{}).Where("eligibility_state = ?", discovery.DiscoveryEligibilityEligible).Pluck("id", &eligible)
	eligibleSet := map[uint]bool{}
	for _, id := range eligible {
		eligibleSet[id] = true
	}
	lowHit := map[uint]bool{}
	for siteID, ids := range siteCandidate {
		eligibleCount := 0
		for id := range ids {
			if eligibleSet[id] {
				eligibleCount++
			}
		}
		if len(ids) >= 5 && float64(eligibleCount)/float64(len(ids)) <= 0.1 {
			lowHit[siteID] = true
		}
	}
	var sites []discovery.DiscoverySite
	db.Find(&sites)
	siteByID := map[uint]discovery.DiscoverySite{}
	for _, site := range sites {
		siteByID[site.ID] = site
	}
	longTailItems, contributingSites, blogrollItems, backfillItems := map[uint]bool{}, map[uint]bool{}, map[uint]bool{}, map[uint]bool{}
	itemIDs := make([]uint, 0, len(positiveItems))
	for id := range positiveItems {
		itemIDs = append(itemIDs, id)
	}
	sort.Slice(itemIDs, func(i, j int) bool { return itemIDs[i] < itemIDs[j] })
	var positiveRows []RecommendationItem
	if len(itemIDs) > 0 {
		db.Where("id IN ?", itemIDs).Find(&positiveRows)
	}
	itemTime := map[uint]time.Time{}
	for _, item := range positiveRows {
		itemTime[item.ID] = item.CreatedAt
	}
	firstGemAt := map[uint]time.Time{}
	for _, itemID := range itemIDs {
		candidateID := positiveItems[itemID]
		for _, row := range byCandidate[candidateID] {
			if occurredAt := itemTime[itemID]; !occurredAt.IsZero() && (firstGemAt[row.SiteID].IsZero() || occurredAt.Before(firstGemAt[row.SiteID])) {
				firstGemAt[row.SiteID] = occurredAt
			}
			if lowHit[row.SiteID] {
				longTailItems[itemID] = true
				contributingSites[row.SiteID] = true
			}
			if siteByID[row.SiteID].Status != discovery.DiscoverySiteStatusSeed {
				blogrollItems[itemID] = true
			}
			method := strings.ToLower(row.DiscoveryMethod)
			if strings.Contains(method, "sitemap") || strings.Contains(method, "archive") || strings.Contains(method, "backfill") {
				backfillItems[itemID] = true
			}
		}
	}
	metrics.LongTail.GemContribution = metricRate(int64(len(longTailItems)), int64(len(positiveItems)))
	metrics.LongTail.ContributingSites = int64(len(contributingSites))
	metrics.LongTail.BlogrollPositiveArticles = int64(len(blogrollItems))
	metrics.LongTail.BackfillPositiveArticles = int64(len(backfillItems))
	var latencyHours float64
	var latencySites int64
	for siteID, occurredAt := range firstGemAt {
		discoveredAt := siteByID[siteID].FirstDiscoveredAt
		if !discoveredAt.IsZero() && !occurredAt.Before(discoveredAt) {
			latencyHours += occurredAt.Sub(discoveredAt).Hours()
			latencySites++
		}
	}
	if latencySites > 0 {
		metrics.LongTail.AverageHoursToFirstGem = latencyHours / float64(latencySites)
	}
	metrics.Candidates.BackfillEligible = int64(len(backfillItems))
	return nil
}
