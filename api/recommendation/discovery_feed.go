package recommendation

import (
	"DataArk/discovery"
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	RecommendationFeedBatchStatusActive   = "active"
	RecommendationFeedBatchStatusReplaced = "replaced"
	recommendationFeedPolicyVersion       = "discovery-feed-v1"
)

type RecommendationFeedSnapshot struct {
	Batch *RecommendationFeedBatch `json:"batch"`
	Items []RecommendationItem     `json:"items"`
}

var recommendationFeedLocks [64]sync.Mutex

func GetCurrentDiscoveryFeed(userID uint) (*RecommendationFeedSnapshot, error) {
	empty := &RecommendationFeedSnapshot{Items: []RecommendationItem{}}
	if db == nil || userID == 0 {
		return empty, nil
	}
	var batch RecommendationFeedBatch
	result := db.Where("user_id = ? AND status = ?", userID, RecommendationFeedBatchStatusActive).
		Order("created_at desc, id desc").Limit(1).Find(&batch)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return empty, nil
	}
	items := make([]RecommendationItem, 0)
	query := db.Table("recommendation_items AS item").Select("item.*").
		Joins("JOIN discovery_candidates AS candidate ON candidate.id = item.candidate_id").
		Where("item.feed_batch_id = ? AND item.user_id = ?", batch.ID, userID).
		Order("item.rank asc")
	query = discovery.ExcludeBlacklistedCandidateDomains(query, "candidate")
	if err := query.Scan(&items).Error; err != nil {
		return nil, err
	}
	if err := attachRecommendationItemCandidates(items, RecommendationDayStatusPublished); err != nil {
		return nil, err
	}
	batch.ActualCount = len(items)
	return &RecommendationFeedSnapshot{Batch: &batch, Items: items}, nil
}

func RefreshDiscoveryFeed(ctx context.Context, userID uint, limit int) (*RecommendationFeedSnapshot, error) {
	if userID == 0 {
		return &RecommendationFeedSnapshot{Items: []RecommendationItem{}}, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	lock := &recommendationFeedLocks[userID%uint(len(recommendationFeedLocks))]
	lock.Lock()
	defer lock.Unlock()
	if db == nil {
		return &RecommendationFeedSnapshot{Items: []RecommendationItem{}}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return nil, err
	}
	profile, err := RebuildUserRecommendationProfile(userID)
	if err != nil {
		return nil, err
	}
	current, err := GetCurrentDiscoveryFeed(userID)
	if err != nil {
		return nil, err
	}

	report, err := selectDailyRecommendationCandidatesV3(ctx, userID, *settings, profile, limit)
	if err != nil {
		return nil, err
	}
	selected, relaxations := diversifyRecommendationCandidatesV3(report.Candidates, limit, settings.ExplorationRate)
	if len(selected) < limit {
		relaxed, relaxedErr := selectRecommendationCandidatesV3(ctx, userID, *settings, profile, limit, recommendationSelectionOptions{AllowRecentExposure: true})
		if relaxedErr != nil {
			return nil, relaxedErr
		}
		beforeFallback := len(selected)
		selected, err = appendDiscoveryFeedFallback(selected, relaxed.Candidates, current.Items, userID, limit)
		if err != nil {
			return nil, err
		}
		if len(selected) > beforeFallback {
			relaxations = append(relaxations, "reexposure_cooldown")
		}
	}

	now := recommendationClock.Now()
	items := buildRecommendationItems(1, userID, selected, 1, profile.ProfileVersion, false, now)
	for index := range items {
		items[index].DayID = nil
	}
	shortage, _ := json.Marshal(map[string]interface{}{
		"target": limit, "actual": len(items), "softRelaxations": relaxations,
	})
	batch := RecommendationFeedBatch{
		UserID: userID, Status: RecommendationFeedBatchStatusActive,
		RequestedCount: limit, ActualCount: len(items), PolicyVersion: recommendationFeedPolicyVersion,
		ProfileVersion: profile.ProfileVersion, ShortageReasons: string(shortage), CreatedAt: now, UpdatedAt: now,
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&RecommendationFeedBatch{}).
			Where("user_id = ? AND status = ?", userID, RecommendationFeedBatchStatusActive).
			Updates(map[string]interface{}{"status": RecommendationFeedBatchStatusReplaced, "replaced_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		for index := range items {
			items[index].FeedBatchID = uintPointer(batch.ID)
			if err := tx.Create(&items[index]).Error; err != nil {
				return err
			}
			if err := discovery.RecordUserCandidateExposure(tx, userID, items[index].CandidateID, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := attachRecommendationItemCandidates(items, RecommendationDayStatusPublished); err != nil {
		return nil, err
	}
	return &RecommendationFeedSnapshot{Batch: &batch, Items: items}, nil
}

func appendDiscoveryFeedFallback(selected []recommendationCandidateScore, candidates []recommendationCandidateScore, current []RecommendationItem, userID uint, limit int) ([]recommendationCandidateScore, error) {
	if len(selected) >= limit {
		return selected, nil
	}
	selectedIdentity := make(map[string]bool, len(selected))
	for _, candidate := range selected {
		selectedIdentity[recommendationCandidateIdentity(candidate.Candidate)] = true
	}
	currentIDs := make(map[uint]bool, len(current))
	for _, item := range current {
		currentIDs[item.CandidateID] = true
	}
	states, err := inventoryUserStates(userID, discoveryCandidatesFromScores(candidates))
	if err != nil {
		return nil, err
	}
	nonCurrent := make([]recommendationCandidateScore, 0, len(candidates))
	fromCurrent := make([]recommendationCandidateScore, 0, len(current))
	for _, candidate := range candidates {
		identity := recommendationCandidateIdentity(candidate.Candidate)
		if selectedIdentity[identity] {
			continue
		}
		candidate.CooldownRepeat = true
		if currentIDs[candidate.Candidate.ID] {
			fromCurrent = append(fromCurrent, candidate)
		} else {
			nonCurrent = append(nonCurrent, candidate)
		}
	}
	sortDiscoveryFeedFallback(nonCurrent, states)
	sortDiscoveryFeedFallback(fromCurrent, states)
	for _, tier := range [][]recommendationCandidateScore{nonCurrent, fromCurrent} {
		for _, candidate := range tier {
			identity := recommendationCandidateIdentity(candidate.Candidate)
			if selectedIdentity[identity] {
				continue
			}
			selected = append(selected, candidate)
			selectedIdentity[identity] = true
			if len(selected) >= limit {
				return selected, nil
			}
		}
	}
	return selected, nil
}

func discoveryCandidatesFromScores(scores []recommendationCandidateScore) []DiscoveryCandidate {
	candidates := make([]DiscoveryCandidate, 0, len(scores))
	for _, score := range scores {
		candidates = append(candidates, score.Candidate)
	}
	return candidates
}

func sortDiscoveryFeedFallback(candidates []recommendationCandidateScore, states map[uint]discovery.UserCandidateState) {
	sort.SliceStable(candidates, func(i, j int) bool {
		first, second := states[candidates[i].Candidate.ID], states[candidates[j].Candidate.ID]
		if first.ExposureCount != second.ExposureCount {
			return first.ExposureCount < second.ExposureCount
		}
		firstAt, secondAt := exposureTime(first), exposureTime(second)
		if !firstAt.Equal(secondAt) {
			return firstAt.Before(secondAt)
		}
		if candidates[i].FinalScore != candidates[j].FinalScore {
			return candidates[i].FinalScore > candidates[j].FinalScore
		}
		return candidates[i].Candidate.ID < candidates[j].Candidate.ID
	})
}

func exposureTime(state discovery.UserCandidateState) time.Time {
	if state.LastExposedAt == nil {
		return time.Time{}
	}
	return *state.LastExposedAt
}
