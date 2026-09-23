package recommendation

import (
	"DataArk/assessment"
	"DataArk/config"
	"DataArk/discovery"
	"time"
)

const (
	CandidateInventoryHealthy  = "healthy"
	CandidateInventoryWarning  = "warning"
	CandidateInventoryCritical = "critical"
)

type CandidateInventory struct {
	UserID                        uint      `json:"userId"`
	EligibleCandidates            int       `json:"eligibleCandidates"`
	FreshEligibleCandidates       int       `json:"freshEligibleCandidates"`
	EvergreenEligibleCandidates   int       `json:"evergreenEligibleCandidates"`
	ExplorationEligibleCandidates int       `json:"explorationEligibleCandidates"`
	UserAvailableCandidates       int       `json:"userAvailableCandidates"`
	UserFreshCandidates           int       `json:"userFreshCandidates"`
	UserEvergreenCandidates       int       `json:"userEvergreenCandidates"`
	UserExplorationCandidates     int       `json:"userExplorationCandidates"`
	DailyLimit                    int       `json:"dailyLimit"`
	InventoryDays                 float64   `json:"inventoryDays"`
	Status                        string    `json:"status"`
	WarningThresholdDays          float64   `json:"warningThresholdDays"`
	CriticalThresholdDays         float64   `json:"criticalThresholdDays"`
	FreshWindowDays               int       `json:"freshWindowDays"`
	ComputedAt                    time.Time `json:"computedAt"`
}

func GetCandidateInventory(userID uint) (*CandidateInventory, error) {
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return nil, err
	}
	var candidates []DiscoveryCandidate
	query := discovery.ExcludeBlacklistedCandidateDomains(db, "")
	if err := query.Where("processing_state = ? AND eligibility_state = ? AND dedupe_state = ? AND (representative_id IS NULL OR representative_id = id)",
		discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible, discovery.DiscoveryDedupeReady).Find(&candidates).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	freshDays := config.DISCOVERYINVENTORYFRESHDAYS
	if freshDays <= 0 {
		freshDays = 30
	}
	freshCutoff := now.AddDate(0, 0, -freshDays)
	exploration, err := explorationCandidateIDs(candidates)
	if err != nil {
		return nil, err
	}
	assessments, err := inventoryAssessments(candidates)
	if err != nil {
		return nil, err
	}
	states, err := inventoryUserStates(userID, candidates)
	if err != nil {
		return nil, err
	}
	rules, err := ListUserBlockRules(userID, true)
	if err != nil {
		return nil, err
	}
	result := &CandidateInventory{
		UserID: userID, DailyLimit: settings.DailyLimit, FreshWindowDays: freshDays, ComputedAt: now,
		WarningThresholdDays: config.DISCOVERYINVENTORYWARNINGDAYS, CriticalThresholdDays: config.DISCOVERYINVENTORYCRITICALDAYS,
	}
	if result.DailyLimit <= 0 {
		result.DailyLimit = 10
	}
	if result.WarningThresholdDays <= 0 {
		result.WarningThresholdDays = 7
	}
	if result.CriticalThresholdDays <= 0 {
		result.CriticalThresholdDays = 3
	}
	seenMaterials := make(map[uint]bool)
	for _, candidate := range candidates {
		if candidate.MaterialID != 0 && seenMaterials[candidate.MaterialID] {
			continue
		}
		seenMaterials[candidate.MaterialID] = true
		fresh := candidate.PublishedAt != nil && !candidate.PublishedAt.Before(freshCutoff)
		evergreen := false
		if row, ok := assessments[candidate.ID]; ok {
			evergreen = row.EvergreenValue >= 0.6
		}
		explore := exploration[candidate.ID]
		result.EligibleCandidates++
		if fresh {
			result.FreshEligibleCandidates++
		}
		if evergreen {
			result.EvergreenEligibleCandidates++
		}
		if explore {
			result.ExplorationEligibleCandidates++
		}
		state, hasState := states[candidate.ID]
		if hasState && userCandidateStateExcludesRecommendation(state) {
			continue
		}
		topics := parseStringList(candidate.Topics)
		if candidateBlocked(candidate, topics, sourceHost(candidate.URL), rules) {
			continue
		}
		result.UserAvailableCandidates++
		if fresh {
			result.UserFreshCandidates++
		}
		if evergreen {
			result.UserEvergreenCandidates++
		}
		if explore || !hasState || state.ExposureCount == 0 {
			result.UserExplorationCandidates++
		}
	}
	result.InventoryDays = float64(result.UserAvailableCandidates) / float64(result.DailyLimit)
	switch {
	case result.InventoryDays < result.CriticalThresholdDays:
		result.Status = CandidateInventoryCritical
	case result.InventoryDays < result.WarningThresholdDays:
		result.Status = CandidateInventoryWarning
	default:
		result.Status = CandidateInventoryHealthy
	}
	return result, nil
}

func explorationCandidateIDs(candidates []DiscoveryCandidate) (map[uint]bool, error) {
	result := make(map[uint]bool)
	if len(candidates) == 0 {
		return result, nil
	}
	ids := make([]uint, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	type row struct{ CandidateID uint }
	var rows []row
	if err := db.Table("material_provenances AS provenance").Select("DISTINCT provenance.candidate_id").
		Joins("JOIN discovery_sites AS site ON site.id = provenance.site_id").
		Where("provenance.candidate_id IN ? AND (site.status = ? OR site.graph_depth > ?)", ids, discovery.DiscoverySiteStatusObserving, 0).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, item := range rows {
		result[item.CandidateID] = true
	}
	return result, nil
}

func inventoryAssessments(candidates []DiscoveryCandidate) (map[uint]assessment.ArticleAssessment, error) {
	result := make(map[uint]assessment.ArticleAssessment)
	ids := make([]uint, 0)
	for _, candidate := range candidates {
		if candidate.CurrentAssessmentID != nil {
			ids = append(ids, *candidate.CurrentAssessmentID)
		}
	}
	if len(ids) == 0 {
		return result, nil
	}
	var rows []assessment.ArticleAssessment
	if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]assessment.ArticleAssessment, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	for _, candidate := range candidates {
		if candidate.CurrentAssessmentID != nil {
			if row, ok := byID[*candidate.CurrentAssessmentID]; ok {
				result[candidate.ID] = row
			}
		}
	}
	return result, nil
}

func inventoryUserStates(userID uint, candidates []DiscoveryCandidate) (map[uint]discovery.UserCandidateState, error) {
	result := make(map[uint]discovery.UserCandidateState)
	if userID == 0 || len(candidates) == 0 {
		return result, nil
	}
	ids := make([]uint, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	var states []discovery.UserCandidateState
	if err := db.Where("user_id = ? AND material_id IN (SELECT material_id FROM discovery_candidates WHERE id IN ?)", userID, ids).Find(&states).Error; err != nil {
		return nil, err
	}
	for _, state := range states {
		for _, candidate := range candidates {
			if candidate.MaterialID == state.MaterialID {
				result[candidate.ID] = state
			}
		}
	}
	return result, nil
}
