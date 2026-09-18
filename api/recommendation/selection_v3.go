package recommendation

import (
	"DataArk/config"
	"DataArk/discovery"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const recommendationSelectionPolicyV3 = "v3-selection-llm-1"

var recommendationClock discovery.Clock = discovery.SystemClock{}

type recommendationSelectionReport struct {
	Candidates        []recommendationCandidateScore
	Excluded          map[string]int
	SoftRelaxations   []string
	EligibleAfterHard int
}

type recommendationSelectionOptions struct {
	UnseenOrUpdatedOnly bool
}

type recommendationHistoryEntry struct {
	LastRecommendedAt time.Time
	MaxContentVersion uint
}

func selectDailyRecommendationCandidates(ctx context.Context, userID uint, settings RecommendationSettings, profile *UserRecommendationProfile, selectionLimit int) ([]recommendationCandidateScore, error) {
	report, err := selectDailyRecommendationCandidatesV3(ctx, userID, settings, profile, selectionLimit)
	if err != nil {
		return nil, err
	}
	return trimRecommendationCandidates(report.Candidates, selectionLimit), nil
}

func selectDailyRecommendationCandidatesV3(ctx context.Context, userID uint, settings RecommendationSettings, profile *UserRecommendationProfile, selectionLimit int) (*recommendationSelectionReport, error) {
	return selectRecommendationCandidatesV3(ctx, userID, settings, profile, selectionLimit, recommendationSelectionOptions{})
}

func selectRecommendationCandidatesV3(ctx context.Context, userID uint, settings RecommendationSettings, profile *UserRecommendationProfile, selectionLimit int, options recommendationSelectionOptions) (*recommendationSelectionReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if selectionLimit <= 0 {
		selectionLimit = maxInt(settings.DailyLimit, 10)
	}
	if profile == nil {
		profile = &UserRecommendationProfile{UserID: userID, DepthPreference: 0.5, ExplorationRate: settings.ExplorationRate}
	}
	poolSize := config.RECOMMENDATIONCANDIDATEPOOLSIZE
	if poolSize < selectionLimit*10 {
		poolSize = selectionLimit * 10
	}
	if poolSize < 100 {
		poolSize = 100
	}
	history, err := loadRecommendationHistoryV3(userID)
	if err != nil {
		return nil, err
	}
	blockRules, err := ListUserBlockRules(userID, true)
	if err != nil {
		return nil, err
	}
	now := recommendationClock.Now()
	cooldown := configuredRecommendationReexposureCooldown()
	candidates, scanExcluded, err := collectHardEligibleSelectionPool(userID, poolSize, options, history, blockRules, now, cooldown)
	if err != nil {
		return nil, err
	}
	report := &recommendationSelectionReport{Candidates: make([]recommendationCandidateScore, 0, len(candidates)), Excluded: scanExcluded}
	if err := populateGlobalSelectionExclusions(report.Excluded); err != nil {
		return nil, err
	}
	states, err := inventoryUserStates(userID, candidates)
	if err != nil {
		return nil, err
	}
	assessments, err := inventoryAssessments(candidates)
	if err != nil {
		return nil, err
	}
	siteExploration, err := explorationCandidateIDs(candidates)
	if err != nil {
		return nil, err
	}
	seenTopics, seenAuthors, err := loadSeenTopicsAndAuthors(userID)
	if err != nil {
		return nil, err
	}
	vectorCandidateIDs, err := loadPGVectorCandidateIDs(ctx, profile, poolSize)
	if err != nil {
		return nil, err
	}
	vectorSeen := make(map[uint]struct{}, len(vectorCandidateIDs))
	for _, id := range vectorCandidateIDs {
		vectorSeen[id] = struct{}{}
	}
	freshDays := settings.CandidateWindowDays
	if freshDays <= 0 {
		freshDays = 30
	}
	freshCutoff := now.AddDate(0, 0, -freshDays)
	seenIdentity := make(map[string]struct{})
	for _, candidate := range candidates {
		state, hasState := states[candidate.ID]
		historyEntry, hasHistory := recommendationHistoryForCandidate(history, candidate)
		contentUpdated, cooldownRepeat, exclusion := recommendationRecurrenceDecision(candidate, state, hasState, historyEntry, hasHistory, now, cooldown)
		if exclusion != "" {
			report.Excluded[exclusion]++
			continue
		}
		topics := parseStringList(candidate.Topics)
		host := sourceHost(candidate.URL)
		if candidateBlocked(candidate, topics, host, blockRules) {
			report.Excluded["user_block"]++
			continue
		}
		identity := recommendationCandidateIdentity(candidate)
		if _, exists := seenIdentity[identity]; exists {
			report.Excluded["same_cluster_daily"]++
			continue
		}
		assessment := assessments[candidate.ID]
		poolTags := make([]string, 0, 3)
		if candidate.PublishedAt != nil && !candidate.PublishedAt.Before(freshCutoff) {
			poolTags = append(poolTags, "fresh")
		}
		if assessment.EvergreenValue >= 0.6 {
			poolTags = append(poolTags, "evergreen")
		}
		exploration, explorationReason := candidateExplorationReason(candidate, topics, state, hasState, siteExploration[candidate.ID], seenTopics, seenAuthors)
		if exploration {
			poolTags = append(poolTags, "exploration")
		}
		if _, ok := vectorSeen[candidate.ID]; ok {
			poolTags = append(poolTags, "vector")
		}
		quality := candidate.QualityScore
		if assessment.ID != 0 {
			quality = assessment.OverallQuality
		}
		poolType := primaryPoolType(poolTags)
		seenIdentity[identity] = struct{}{}
		report.Candidates = append(report.Candidates, recommendationCandidateScore{
			Candidate: candidate, Topics: topics, SourceHost: host, Author: strings.TrimSpace(candidate.Author),
			PoolTags: poolTags, PoolType: poolType, Exploration: exploration, ExplorationReason: explorationReason,
			ContentUpdated: contentUpdated, CooldownRepeat: cooldownRepeat,
			RetrievalScore: quality, FinalScore: quality, Reason: "",
		})
	}
	sort.SliceStable(report.Candidates, func(i, j int) bool {
		if report.Candidates[i].FinalScore != report.Candidates[j].FinalScore {
			return report.Candidates[i].FinalScore > report.Candidates[j].FinalScore
		}
		leftSeen := report.Candidates[i].Candidate.LastSeenAt
		rightSeen := report.Candidates[j].Candidate.LastSeenAt
		if !leftSeen.Equal(rightSeen) {
			return leftSeen.After(rightSeen)
		}
		return report.Candidates[i].Candidate.ID < report.Candidates[j].Candidate.ID
	})
	report.EligibleAfterHard = len(report.Candidates)
	return report, nil
}

// collectHardEligibleSelectionPool 按质量排序分页扫描，直到凑满 poolSize 篇通过硬过滤的候选。
// 来源屏蔽、已读和冷却不能再占住第一页，把后面仍合格的文章挡在 LIMIT 之外。
// 组池时每个来源先只收一篇，避免单一高产来源占满前 100 篇后，多样化误以为没有其他来源而放宽限制。
func collectHardEligibleSelectionPool(userID uint, poolSize int, options recommendationSelectionOptions, history map[string]recommendationHistoryEntry, blockRules []UserBlockRule, now time.Time, cooldown time.Duration) ([]DiscoveryCandidate, map[string]int, error) {
	excluded := make(map[string]int)
	if poolSize <= 0 {
		return []DiscoveryCandidate{}, excluded, nil
	}
	unique := make([]DiscoveryCandidate, 0, poolSize)
	overflow := make([]DiscoveryCandidate, 0, poolSize)
	sourceSeen := make(map[string]bool)
	offset := 0
	scanned := 0
	maxScan := poolSize * 50
	if maxScan < 2000 {
		maxScan = 2000
	}
	pageLimit := poolSize * 4
	if pageLimit < poolSize {
		pageLimit = poolSize
	}
	for len(unique) < poolSize && scanned < maxScan {
		query := recommendationEligibleCandidateQuery(userID, options.UnseenOrUpdatedOnly)
		var page []DiscoveryCandidate
		if err := query.Offset(offset).Limit(pageLimit).Find(&page).Error; err != nil {
			return nil, nil, err
		}
		if len(page) == 0 {
			break
		}
		states, err := inventoryUserStates(userID, page)
		if err != nil {
			return nil, nil, err
		}
		for _, candidate := range page {
			scanned++
			if scanned > maxScan {
				break
			}
			state, hasState := states[candidate.ID]
			historyEntry, hasHistory := recommendationHistoryForCandidate(history, candidate)
			_, _, exclusion := recommendationRecurrenceDecision(candidate, state, hasState, historyEntry, hasHistory, now, cooldown)
			if exclusion != "" {
				excluded[exclusion]++
				continue
			}
			if candidateBlocked(candidate, parseStringList(candidate.Topics), sourceHost(candidate.URL), blockRules) {
				excluded["user_block"]++
				continue
			}
			key := recommendationSourceKeyFromCandidate(candidate)
			if !sourceSeen[key] {
				unique = append(unique, candidate)
				sourceSeen[key] = true
				if len(unique) >= poolSize {
					break
				}
				continue
			}
			if len(overflow) < poolSize {
				overflow = append(overflow, candidate)
			}
		}
		offset += len(page)
		if len(page) < pageLimit {
			break
		}
	}
	selected := unique
	if len(selected) < poolSize {
		for _, candidate := range overflow {
			if len(selected) >= poolSize {
				break
			}
			selected = append(selected, candidate)
		}
	}
	return selected, excluded, nil
}

// recommendationEligibleCandidateQuery 构造推荐硬合格代表的有序查询；每页从干净的 DB 句柄重建，避免 GORM Where 累积。
func recommendationEligibleCandidateQuery(userID uint, unseenOrUpdatedOnly bool) *gorm.DB {
	query := discovery.ExcludeBlacklistedCandidateDomains(db, "")
	if unseenOrUpdatedOnly {
		query = query.Select("discovery_candidates.*").
			Joins("LEFT JOIN user_candidate_states AS candidate_exposure ON candidate_exposure.user_id = ? AND candidate_exposure.candidate_id = discovery_candidates.id", userID).
			Where(`candidate_exposure.id IS NULL OR candidate_exposure.exposure_count = 0 OR EXISTS (
SELECT 1
FROM recommendation_items AS prior_item
WHERE prior_item.user_id = ?
  AND prior_item.content_version < discovery_candidates.content_version
  AND (
		prior_item.candidate_id = discovery_candidates.id OR
		(discovery_candidates.dedupe_key <> '' AND prior_item.dedupe_key = discovery_candidates.dedupe_key)
  )
)`, userID)
	}
	return query.Where("discovery_candidates.processing_state = ? AND discovery_candidates.eligibility_state = ?", discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible).
		Where("discovery_candidates.dedupe_state = ? AND (discovery_candidates.representative_id IS NULL OR discovery_candidates.representative_id = discovery_candidates.id)", discovery.DiscoveryDedupeReady).
		Order("discovery_candidates.quality_score desc, discovery_candidates.last_seen_at desc, discovery_candidates.id asc")
}

func populateGlobalSelectionExclusions(excluded map[string]int) error {
	var blacklisted int64
	if err := discovery.OnlyBlacklistedCandidateDomains(db.Model(&DiscoveryCandidate{}), "").Count(&blacklisted).Error; err != nil {
		return err
	}
	excluded["domain_blacklist"] = int(blacklisted)
	queries := []struct {
		key   string
		where string
		args  []interface{}
	}{
		{"processing_not_ready", "processing_state <> ?", []interface{}{discovery.DiscoveryProcessingReady}},
		{"article_ineligible", "processing_state = ? AND eligibility_state <> ?", []interface{}{discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible}},
		{"duplicate_non_representative", "processing_state = ? AND eligibility_state = ? AND (dedupe_state <> ? OR (representative_id IS NOT NULL AND representative_id <> id))", []interface{}{discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible, discovery.DiscoveryDedupeReady}},
	}
	for _, query := range queries {
		var count int64
		if err := db.Model(&DiscoveryCandidate{}).Where(query.where, query.args...).Count(&count).Error; err != nil {
			return err
		}
		excluded[query.key] = int(count)
	}
	return nil
}

func loadRecommendationHistoryV3(userID uint) (map[string]recommendationHistoryEntry, error) {
	result := make(map[string]recommendationHistoryEntry)
	var items []RecommendationItem
	if err := db.Table("recommendation_items AS item").Select("item.*").
		Joins("LEFT JOIN recommendation_days AS day ON day.id = item.day_id").
		Where("item.user_id = ?", userID).
		Where("item.feed_batch_id IS NOT NULL OR day.status IN ?", []string{RecommendationDayStatusPublished, RecommendationDayStatusSupplemented, "generated"}).
		Order("item.created_at asc, item.id asc").Scan(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		entry := recommendationHistoryEntry{LastRecommendedAt: item.CreatedAt, MaxContentVersion: item.ContentVersion}
		mergeRecommendationHistory(result, fmt.Sprintf("candidate:%d", item.CandidateID), entry)
		if key := strings.TrimSpace(item.DedupeKey); key != "" {
			mergeRecommendationHistory(result, "dedupe:"+key, entry)
		}
	}
	return result, nil
}

func mergeRecommendationHistory(history map[string]recommendationHistoryEntry, key string, entry recommendationHistoryEntry) {
	existing := history[key]
	if entry.LastRecommendedAt.After(existing.LastRecommendedAt) {
		existing.LastRecommendedAt = entry.LastRecommendedAt
	}
	if entry.MaxContentVersion > existing.MaxContentVersion {
		existing.MaxContentVersion = entry.MaxContentVersion
	}
	history[key] = existing
}

func recommendationHistoryForCandidate(history map[string]recommendationHistoryEntry, candidate DiscoveryCandidate) (recommendationHistoryEntry, bool) {
	entry, ok := history[fmt.Sprintf("candidate:%d", candidate.ID)]
	if key := strings.TrimSpace(candidate.DedupeKey); key != "" {
		if dedupe, exists := history["dedupe:"+key]; exists {
			if !ok || dedupe.LastRecommendedAt.After(entry.LastRecommendedAt) {
				entry.LastRecommendedAt = dedupe.LastRecommendedAt
			}
			if dedupe.MaxContentVersion > entry.MaxContentVersion {
				entry.MaxContentVersion = dedupe.MaxContentVersion
			}
			ok = true
		}
	}
	return entry, ok
}

func recommendationRecurrenceDecision(candidate DiscoveryCandidate, state discovery.UserCandidateState, hasState bool, history recommendationHistoryEntry, hasHistory bool, now time.Time, cooldown time.Duration) (bool, bool, string) {
	if hasState {
		if state.ArchivedAt != nil || state.DeepReadAt != nil || strings.TrimSpace(state.CurrentFeedback) != "" {
			return false, false, "explicit_user_state"
		}
	}
	if !hasHistory && (!hasState || state.ExposureCount == 0) {
		return false, false, ""
	}
	updated := hasHistory && candidate.ContentVersion > history.MaxContentVersion
	if updated {
		return true, false, ""
	}
	if hasState && (state.OpenedAt != nil || state.ReadAt != nil) {
		return false, false, "opened_or_read"
	}
	lastExposure := history.LastRecommendedAt
	if hasState && state.LastExposedAt != nil && state.LastExposedAt.After(lastExposure) {
		lastExposure = *state.LastExposedAt
	}
	if lastExposure.IsZero() || now.Sub(lastExposure) < cooldown {
		return false, false, "reexposure_cooldown"
	}
	return false, true, ""
}

func configuredRecommendationReexposureCooldown() time.Duration {
	cooldown, err := time.ParseDuration(strings.TrimSpace(config.RECOMMENDATIONREEXPOSURECOOLDOWN))
	if err != nil || cooldown < 60*24*time.Hour || cooldown > 90*24*time.Hour {
		return 75 * 24 * time.Hour
	}
	return cooldown
}

func loadSeenTopicsAndAuthors(userID uint) (map[string]bool, map[string]bool, error) {
	topics := make(map[string]bool)
	authors := make(map[string]bool)
	var items []RecommendationItem
	if err := db.Table("recommendation_items AS item").Select("item.*").
		Joins("JOIN recommendation_days AS day ON day.id = item.day_id").
		Where("item.user_id = ? AND day.status IN ?", userID, []string{RecommendationDayStatusPublished, RecommendationDayStatusSupplemented, "generated"}).
		Scan(&items).Error; err != nil {
		return nil, nil, err
	}
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.CandidateID)
		if author := strings.ToLower(strings.TrimSpace(item.SnapshotAuthor)); author != "" {
			authors[author] = true
		}
	}
	if len(ids) > 0 {
		var candidates []DiscoveryCandidate
		if err := db.Where("id IN ?", ids).Find(&candidates).Error; err != nil {
			return nil, nil, err
		}
		for _, candidate := range candidates {
			for _, topic := range parseStringList(candidate.Topics) {
				topics[strings.ToLower(topic)] = true
			}
			if author := strings.ToLower(strings.TrimSpace(candidate.Author)); author != "" {
				authors[author] = true
			}
		}
	}
	return topics, authors, nil
}

func candidateExplorationReason(candidate DiscoveryCandidate, topics []string, state discovery.UserCandidateState, hasState bool, siteExploration bool, seenTopics map[string]bool, seenAuthors map[string]bool) (bool, string) {
	if siteExploration {
		return true, "new_or_long_tail_site"
	}
	if author := strings.ToLower(strings.TrimSpace(candidate.Author)); author != "" && !seenAuthors[author] {
		return true, "new_author"
	}
	for _, topic := range topics {
		if !seenTopics[strings.ToLower(topic)] {
			return true, "new_topic"
		}
	}
	if !hasState || state.ExposureCount == 0 {
		return true, "low_user_exposure"
	}
	return false, ""
}

func primaryPoolType(tags []string) string {
	for _, preferred := range []string{"fresh", "evergreen", "exploration"} {
		for _, tag := range tags {
			if tag == preferred {
				return preferred
			}
		}
	}
	return "eligible"
}

// recommendationSourceKey 用卡片上展示的来源名（或 URL 主机）标识来源，优先保证每批同一来源只出现一次。
func recommendationSourceKey(candidate DiscoveryCandidate, host string) string {
	key := strings.ToLower(strings.TrimSpace(firstNonEmpty(candidate.SourceName, host)))
	if key != "" {
		return key
	}
	return fmt.Sprintf("candidate:%d", candidate.ID)
}

func recommendationSourceKeyFromCandidate(candidate DiscoveryCandidate) string {
	return recommendationSourceKey(candidate, firstNonEmpty(candidate.CrawlHost, sourceHost(candidate.URL)))
}

func recommendationSourceKeyFromItem(item RecommendationItem) string {
	host := sourceHost(firstNonEmpty(item.SnapshotURL, item.Candidate.URL))
	name := firstNonEmpty(item.SnapshotSource, item.Candidate.SourceName)
	return recommendationSourceKey(DiscoveryCandidate{ID: item.CandidateID, SourceName: name}, host)
}

func recommendationCandidateIdentity(candidate DiscoveryCandidate) string {
	if cluster := strings.TrimSpace(candidate.DuplicateClusterID); cluster != "" {
		return "cluster:" + cluster
	}
	if key := strings.TrimSpace(candidate.DedupeKey); key != "" {
		return "dedupe:" + key
	}
	return fmt.Sprintf("candidate:%d", candidate.ID)
}

func marshalSelectionAudit(target int, actual int, report *recommendationSelectionReport, relaxations []string) string {
	if report == nil {
		return ""
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"target": target, "actual": actual, "eligibleAfterHardFilters": report.EligibleAfterHard,
		"excluded": report.Excluded, "softRelaxations": relaxations,
	})
	return string(payload)
}
