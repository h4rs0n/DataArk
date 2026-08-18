package recommendation

import (
	"DataArk/config"
	"DataArk/discovery"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const recommendationSelectionPolicyV3 = "v3-selection-3"

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

type recommendationScoreInput struct {
	Quality         float64
	Depth           float64
	Evergreen       float64
	PublishedAt     *time.Time
	Topics          []string
	ContentStyle    string
	ContentType     string
	TopicWeights    map[string]float64
	StyleWeights    map[string]float64
	DepthPreference float64
	Now             time.Time
}

func selectDailyRecommendationCandidates(ctx context.Context, userID uint, settings RecommendationSettings, profile *UserRecommendationProfile, selectionLimit int) ([]recommendationCandidateScore, error) {
	report, err := selectDailyRecommendationCandidatesV3(ctx, userID, settings, profile, selectionLimit)
	if err != nil {
		return nil, err
	}
	selected, relaxations := diversifyRecommendationCandidatesV3(report.Candidates, selectionLimit, settings.ExplorationRate)
	report.SoftRelaxations = relaxations
	return selected, nil
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
	vectorBoosts := make(map[uint]float64, len(vectorCandidateIDs))
	for index, id := range vectorCandidateIDs {
		vectorBoosts[id] = 0.25 * (1 - float64(index)/float64(len(vectorCandidateIDs)+1))
	}
	topicWeights := parseWeightMap(profile.TopicWeights)
	styleWeights := parseWeightMap(profile.StyleWeights)
	preferredLanguages := normalizedPreferenceSet(parseStringList(settings.PreferredLanguages))
	favoriteSources := normalizedPreferenceSet(parseStringList(settings.FavoriteSources))
	freshDays := settings.CandidateWindowDays
	if freshDays <= 0 {
		freshDays = 30
	}
	freshCutoff := now.AddDate(0, 0, -freshDays)
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
		quality := candidate.QualityScore
		depth := candidate.DepthScore
		if assessment.ID != 0 {
			quality = assessment.OverallQuality
			depth = assessment.Depth
		}
		score := scoreRecommendationCandidateV3(recommendationScoreInput{
			Quality: quality, Depth: depth, Evergreen: assessment.EvergreenValue, PublishedAt: candidate.PublishedAt,
			Topics: topics, ContentStyle: candidate.ContentStyle, ContentType: candidate.ContentType,
			TopicWeights: topicWeights, StyleWeights: styleWeights, DepthPreference: profile.DepthPreference, Now: now,
		})
		score += vectorBoosts[candidate.ID]
		if _, ok := preferredLanguages[strings.ToLower(strings.TrimSpace(candidate.Language))]; ok {
			score += 0.05
		}
		switch settings.PreferredLength {
		case "short":
			if candidate.WordCount > 0 && candidate.WordCount <= 1200 {
				score += 0.05
			}
		case "long":
			if candidate.WordCount >= 1800 {
				score += 0.05
			}
		}
		if explicitSourcePreferenceMatches(candidate, host, favoriteSources) {
			score += 0.08
		}
		if exploration {
			score += 0.02
		}
		poolType := primaryPoolType(poolTags)
		report.Candidates = append(report.Candidates, recommendationCandidateScore{
			Candidate: candidate, Topics: topics, SourceHost: host, Author: strings.TrimSpace(candidate.Author),
			PoolTags: poolTags, PoolType: poolType, Exploration: exploration, ExplorationReason: explorationReason,
			ContentUpdated: contentUpdated, CooldownRepeat: cooldownRepeat,
			RetrievalScore: score, FinalScore: score, Reason: recommendationReasonV3(candidate, topics, poolType, explorationReason, contentUpdated),
		})
	}
	sort.SliceStable(report.Candidates, func(i, j int) bool {
		if report.Candidates[i].FinalScore != report.Candidates[j].FinalScore {
			return report.Candidates[i].FinalScore > report.Candidates[j].FinalScore
		}
		return report.Candidates[i].Candidate.ID < report.Candidates[j].Candidate.ID
	})
	report.EligibleAfterHard = len(report.Candidates)
	return report, nil
}

func normalizedPreferenceSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			result[value] = struct{}{}
		}
	}
	return result
}

// collectHardEligibleSelectionPool 按质量排序分页扫描，直到凑满 poolSize 篇通过硬过滤的候选。
// 来源屏蔽、已读和冷却不能再占住第一页，把后面仍合格的文章挡在 LIMIT 之外。
func collectHardEligibleSelectionPool(userID uint, poolSize int, options recommendationSelectionOptions, history map[string]recommendationHistoryEntry, blockRules []UserBlockRule, now time.Time, cooldown time.Duration) ([]DiscoveryCandidate, map[string]int, error) {
	excluded := make(map[string]int)
	if poolSize <= 0 {
		return []DiscoveryCandidate{}, excluded, nil
	}
	selected := make([]DiscoveryCandidate, 0, poolSize)
	offset := 0
	for len(selected) < poolSize {
		query := recommendationEligibleCandidateQuery(userID, options.UnseenOrUpdatedOnly)
		var page []DiscoveryCandidate
		if err := query.Offset(offset).Limit(poolSize).Find(&page).Error; err != nil {
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
			selected = append(selected, candidate)
			if len(selected) >= poolSize {
				break
			}
		}
		offset += len(page)
		if len(page) < poolSize {
			break
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
		Order("discovery_candidates.quality_score desc, discovery_candidates.depth_score desc, discovery_candidates.score desc, discovery_candidates.last_seen_at desc, discovery_candidates.id asc")
}

func explicitSourcePreferenceMatches(candidate DiscoveryCandidate, host string, favorites map[string]struct{}) bool {
	if len(favorites) == 0 {
		return false
	}
	for _, value := range []string{candidate.SourceName, host} {
		if _, ok := favorites[strings.ToLower(strings.TrimSpace(value))]; ok {
			return true
		}
	}
	return false
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

func scoreRecommendationCandidateV3(input recommendationScoreInput) float64 {
	score := 0.12 + clampScore(input.Quality)*0.35 + clampScore(input.Depth)*0.15 + freshnessScoreAt(input.PublishedAt, input.Now)*0.13 + clampScore(input.Evergreen)*0.08
	for _, topic := range input.Topics {
		score += boundedWeight(input.TopicWeights[topic]) * 0.16
	}
	for _, style := range []string{input.ContentStyle, input.ContentType} {
		score += boundedWeight(input.StyleWeights[style]) * 0.07
	}
	if input.DepthPreference > 0.5 {
		score += clampScore(input.Depth) * (input.DepthPreference - 0.5) * 0.2
	}
	return score
}

func freshnessScoreAt(publishedAt *time.Time, now time.Time) float64 {
	if publishedAt == nil {
		return 0.35
	}
	age := now.Sub(*publishedAt)
	switch {
	case age <= 24*time.Hour:
		return 1
	case age <= 7*24*time.Hour:
		return 0.75
	case age <= 30*24*time.Hour:
		return 0.45
	default:
		return 0.15
	}
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

func recommendationReasonV3(candidate DiscoveryCandidate, topics []string, poolType string, explorationReason string, updated bool) string {
	if updated {
		return "文章正文有实质更新，可重新阅读"
	}
	if explorationReason != "" {
		return "探索推荐：" + explorationReason
	}
	if len(topics) > 0 {
		return "基于文章质量、" + poolType + " 属性和主题 " + topics[0] + " 推荐"
	}
	return "基于文章质量和 " + poolType + " 属性推荐"
}

func diversifyRecommendationCandidatesV3(candidates []recommendationCandidateScore, limit int, explorationRate float64) ([]recommendationCandidateScore, []string) {
	return diversifyRecommendationCandidatesV3WithReserved(candidates, limit, explorationRate, nil)
}

func diversifyRecommendationCandidatesV3WithReserved(candidates []recommendationCandidateScore, limit int, explorationRate float64, reservedSources map[string]int) ([]recommendationCandidateScore, []string) {
	if limit <= 0 || len(candidates) == 0 {
		return []recommendationCandidateScore{}, []string{}
	}
	if explorationRate < 0 {
		explorationRate = 0
	}
	if explorationRate > 1 {
		explorationRate = 1
	}
	explorationAvailable := 0
	for _, candidate := range candidates {
		if candidate.Exploration {
			explorationAvailable++
		}
	}
	explorationTarget := int(math.Ceil(float64(limit) * explorationRate))
	if limit >= 5 && explorationAvailable > 0 && explorationTarget < 1 {
		explorationTarget = 1
	}
	if explorationTarget > explorationAvailable {
		explorationTarget = explorationAvailable
	}
	if explorationTarget > limit {
		explorationTarget = limit
	}
	// 每个来源先只选一篇；来源不足以填满目标条数时，再放宽允许同一来源多篇。
	maxSource := 1
	maxTopic := maxInt(1, int(math.Ceil(float64(limit)*0.4)))
	maxAuthor := maxInt(1, int(math.Ceil(float64(limit)*0.2)))
	selected := make([]recommendationCandidateScore, 0, limit)
	used := make(map[int]bool)
	usedIdentity := make(map[string]bool)
	sourceCounts := make(map[string]int)
	for source, count := range reservedSources {
		if count > 0 {
			sourceCounts[source] = count
		}
	}
	topicCounts := make(map[string]int)
	authorCounts := make(map[string]int)
	poolCounts := make(map[string]int)
	selectedExploration := 0
	relaxAuthor, relaxTopic, relaxSource := false, false, false
	relaxations := make([]string, 0, 3)
	for len(selected) < limit {
		remaining := limit - len(selected)
		needExploration := selectedExploration < explorationTarget && remaining <= explorationTarget-selectedExploration
		bestIndex := -1
		bestScore := -math.MaxFloat64
		for index, candidate := range candidates {
			if used[index] || usedIdentity[recommendationCandidateIdentity(candidate.Candidate)] || (needExploration && !candidate.Exploration) {
				continue
			}
			source := recommendationSourceKeyFromScore(candidate)
			author := strings.ToLower(candidate.Author)
			if !relaxSource && sourceCounts[source] >= maxSource {
				continue
			}
			if !relaxTopic && dominantTopicCount(candidate.Topics, topicCounts) >= maxTopic {
				continue
			}
			if !relaxAuthor && author != "" && authorCounts[author] >= maxAuthor {
				continue
			}
			score := candidateSelectionScore(candidate) - maxSimilarityPenalty(candidate, selected)
			if poolCounts[candidate.PoolType] >= maxInt(1, limit/2) {
				score -= 0.03
			}
			if bestIndex == -1 || score > bestScore || (score == bestScore && candidate.Candidate.ID < candidates[bestIndex].Candidate.ID) {
				bestIndex, bestScore = index, score
			}
		}
		if bestIndex == -1 {
			canRelaxAuthor, canRelaxTopic, canRelaxSource := false, false, false
			for index, candidate := range candidates {
				if used[index] || usedIdentity[recommendationCandidateIdentity(candidate.Candidate)] {
					continue
				}
				sourceBlocked := !relaxSource && sourceCounts[recommendationSourceKeyFromScore(candidate)] >= maxSource
				if sourceBlocked {
					canRelaxSource = true
					continue
				}
				author := strings.ToLower(candidate.Author)
				if !relaxAuthor && author != "" && authorCounts[author] >= maxAuthor {
					canRelaxAuthor = true
				}
				if !relaxTopic && dominantTopicCount(candidate.Topics, topicCounts) >= maxTopic {
					canRelaxTopic = true
				}
			}
			switch {
			case !relaxAuthor && canRelaxAuthor:
				relaxAuthor = true
				relaxations = append(relaxations, "author_limit")
			case !relaxTopic && canRelaxTopic:
				relaxTopic = true
				relaxations = append(relaxations, "topic_limit")
			case !relaxSource && canRelaxSource:
				relaxSource = true
				relaxations = append(relaxations, "source_limit")
			default:
				return selected, relaxations
			}
			continue
		}
		chosen := candidates[bestIndex]
		chosen.FinalScore = bestScore
		if chosen.Exploration {
			selectedExploration++
			chosen.PoolType = "exploration"
		}
		selected = append(selected, chosen)
		used[bestIndex] = true
		usedIdentity[recommendationCandidateIdentity(chosen.Candidate)] = true
		sourceCounts[recommendationSourceKeyFromScore(chosen)]++
		for _, topic := range chosen.Topics {
			topicCounts[topic]++
		}
		if author := strings.ToLower(chosen.Author); author != "" {
			authorCounts[author]++
		}
		poolCounts[chosen.PoolType]++
	}
	return selected, relaxations
}

// recommendationSourceKey 用卡片上展示的来源名（或 URL 主机）标识来源，优先保证每批同一来源只出现一次。
func recommendationSourceKey(candidate DiscoveryCandidate, host string) string {
	key := strings.ToLower(strings.TrimSpace(firstNonEmpty(candidate.SourceName, host)))
	if key != "" {
		return key
	}
	return fmt.Sprintf("candidate:%d", candidate.ID)
}

func recommendationSourceKeyFromScore(candidate recommendationCandidateScore) string {
	return recommendationSourceKey(candidate.Candidate, candidate.SourceHost)
}

func recommendationSourceKeyFromItem(item RecommendationItem) string {
	host := sourceHost(firstNonEmpty(item.SnapshotURL, item.Candidate.URL))
	name := firstNonEmpty(item.SnapshotSource, item.Candidate.SourceName)
	return recommendationSourceKey(DiscoveryCandidate{ID: item.CandidateID, SourceName: name}, host)
}

// recommendationSourceCountsFromItems 统计当前推荐里各来源已占用的篇数，补篇时用来优先选尚未出现的来源。
func recommendationSourceCountsFromItems(items []RecommendationItem) map[string]int {
	counts := make(map[string]int, len(items))
	for _, item := range items {
		counts[recommendationSourceKeyFromItem(item)]++
	}
	return counts
}

func candidateSelectionScore(candidate recommendationCandidateScore) float64 {
	if candidate.RerankRank > 0 {
		return 1000 - float64(candidate.RerankRank) + candidate.FinalScore/100
	}
	return candidate.FinalScore
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
