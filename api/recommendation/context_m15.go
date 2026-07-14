package recommendation

import (
	"DataArk/discovery"

	"gorm.io/gorm"
)

type RecommendationProvenanceContext struct {
	Provenance discovery.DiscoveryCandidateProvenance `json:"provenance"`
	Site       discovery.DiscoverySite                `json:"site"`
	Source     *discovery.DiscoverySource             `json:"source,omitempty"`
	Graph      *discovery.SiteGraphView               `json:"graph,omitempty"`
}

type RecommendationItemContext struct {
	Item       RecommendationItem                    `json:"item"`
	Feedback   *RecommendationFeedback               `json:"feedback,omitempty"`
	UserState  *discovery.UserCandidateState         `json:"userState,omitempty"`
	Assessment *discovery.DiscoveryArticleAssessment `json:"assessment,omitempty"`
	Provenance []RecommendationProvenanceContext     `json:"provenance"`
}

func GetRecommendationItemContext(userID uint, itemID uint) (*RecommendationItemContext, error) {
	if db == nil || userID == 0 || itemID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var item RecommendationItem
	if err := db.Where("id = ? AND user_id = ?", itemID, userID).First(&item).Error; err != nil {
		return nil, err
	}
	result := &RecommendationItemContext{Item: item, Provenance: make([]RecommendationProvenanceContext, 0)}
	result.Feedback, _ = GetCurrentRecommendationFeedback(userID, itemID)
	var state discovery.UserCandidateState
	if query := db.Where("user_id = ? AND candidate_id = ?", userID, item.CandidateID).Limit(1).Find(&state); query.Error != nil {
		return nil, query.Error
	} else if query.RowsAffected > 0 {
		result.UserState = &state
	}
	if item.AssessmentID != nil {
		var assessment discovery.DiscoveryArticleAssessment
		if query := db.Where("id = ?", *item.AssessmentID).Limit(1).Find(&assessment); query.Error != nil {
			return nil, query.Error
		} else if query.RowsAffected > 0 {
			result.Assessment = &assessment
		}
	}
	var rows []discovery.DiscoveryCandidateProvenance
	if err := db.Where("candidate_id = ?", item.CandidateID).Order("first_seen_at asc, id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		entry := RecommendationProvenanceContext{Provenance: row}
		if err := db.First(&entry.Site, row.SiteID).Error; err != nil {
			return nil, err
		}
		if row.SourceID != nil {
			var source discovery.DiscoverySource
			if query := db.Where("id = ?", *row.SourceID).Limit(1).Find(&source); query.Error != nil {
				return nil, query.Error
			} else if query.RowsAffected > 0 {
				entry.Source = &source
			}
		}
		entry.Graph, _ = discovery.GetSiteGraph(row.SiteID)
		result.Provenance = append(result.Provenance, entry)
	}
	return result, nil
}
