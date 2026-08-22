package recommendation

import (
	"DataArk/discovery"
	"encoding/json"
	"math"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// profile.go 负责根据反馈与互动重建或重置用户推荐画像。

// RebuildUserRecommendationProfile 根据反馈与互动重建用户推荐画像。
func RebuildUserRecommendationProfile(userID uint) (*UserRecommendationProfile, error) {
	if db == nil || userID == 0 {
		return &UserRecommendationProfile{UserID: userID, ProfileVersion: 1}, nil
	}
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return nil, err
	}
	var existing UserRecommendationProfile
	existingResult := db.Where("user_id = ?", userID).Limit(1).Find(&existing)
	if existingResult.Error != nil {
		return nil, existingResult.Error
	}
	var feedback []RecommendationFeedback
	feedbackQuery := db.Where("user_id = ? AND is_current = ? AND reverted_at IS NULL", userID, true)
	if existing.FeedbackResetAt != nil {
		feedbackQuery = feedbackQuery.Where("created_at > ?", *existing.FeedbackResetAt)
	}
	if err := feedbackQuery.Order("created_at asc").Find(&feedback).Error; err != nil {
		return nil, err
	}
	var engagementStates []discovery.UserCandidateState
	if err := db.Where("user_id = ? AND (opened_at IS NOT NULL OR deep_read_at IS NOT NULL OR archived_at IS NOT NULL)", userID).Find(&engagementStates).Error; err != nil {
		return nil, err
	}
	candidateIDs := make([]uint, 0, len(feedback)+len(engagementStates))
	feedbackCandidates := make(map[uint]struct{}, len(feedback))
	for _, item := range feedback {
		candidateIDs = append(candidateIDs, item.CandidateID)
		feedbackCandidates[item.CandidateID] = struct{}{}
	}
	for _, state := range engagementStates {
		if _, hasFeedback := feedbackCandidates[state.CandidateID]; !hasFeedback {
			candidateIDs = append(candidateIDs, state.CandidateID)
		}
	}
	candidates := make(map[uint]DiscoveryCandidate)
	if len(candidateIDs) > 0 {
		var rows []DiscoveryCandidate
		if err := db.Where("id IN ?", candidateIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, candidate := range rows {
			candidates[candidate.ID] = candidate
		}
	}
	embeddingVectors, err := loadCandidateEmbeddingVectors(candidateIDs)
	if err != nil {
		return nil, err
	}
	topicWeights := make(map[string]float64)
	sourceWeights := make(map[string]float64) // Intentionally explicit-only; article feedback never writes here.
	styleWeights := make(map[string]float64)
	positiveVectors := make([][]float32, 0)
	positiveVectorWeights := make([]float64, 0)
	negativeVectors := make([][]float32, 0)
	negativeVectorWeights := make([]float64, 0)
	depthPreference := 0.5
	if settings.PreferredLength == "short" {
		depthPreference = 0.3
	} else if settings.PreferredLength == "long" {
		depthPreference = 0.75
	}
	if settings.PreferredDepth > 0 {
		depthPreference = settings.PreferredDepth
	}
	for _, topic := range parseStringList(settings.PreferredTopics) {
		topicWeights[topic] = 0.75
	}
	now := recommendationClock.Now()
	for _, event := range feedback {
		candidate, ok := candidates[event.CandidateID]
		if !ok {
			continue
		}
		decay := feedbackDecay(event.CreatedAt, now)
		topicDelta, _, styleDelta, depthDelta := feedbackDeltas(event.Action)
		topicDelta *= decay
		styleDelta *= decay
		depthDelta *= decay
		topics := parseStringList(candidate.Topics)
		styles := []string{candidate.ContentStyle, candidate.ContentType}
		if event.Action == RecommendationFeedbackReduceTopic || event.Action == RecommendationFeedbackReduceStyle {
			var metadata recommendationFeedbackMetadata
			_ = json.Unmarshal([]byte(event.Metadata), &metadata)
			topics = nil
			styles = nil
			for _, target := range metadata.BlockTargets {
				if event.Action == RecommendationFeedbackReduceTopic && target.Type == UserBlockRuleTopic {
					topics = append(topics, target.Value)
				}
				if event.Action == RecommendationFeedbackReduceStyle && target.Type == UserBlockRuleStyle {
					styles = append(styles, target.Value)
				}
			}
		}
		for _, topic := range topics {
			if topicDelta != 0 {
				topicWeights[topic] += topicDelta
			}
		}
		for _, style := range styles {
			style = strings.TrimSpace(style)
			if style != "" && styleDelta != 0 {
				styleWeights[style] += styleDelta
			}
		}
		if vector := embeddingVectors[event.CandidateID]; len(vector) > 0 {
			switch normalizeFeedbackAction(event.Action) {
			case RecommendationFeedbackValuable, RecommendationFeedbackDeepRead:
				positiveVectors = append(positiveVectors, vector)
				positiveVectorWeights = append(positiveVectorWeights, decay)
			case RecommendationFeedbackNotInterested:
				negativeVectors = append(negativeVectors, vector)
				negativeVectorWeights = append(negativeVectorWeights, decay)
			}
		}
		depthPreference += depthDelta
	}
	for _, state := range engagementStates {
		if _, hasFeedback := feedbackCandidates[state.CandidateID]; hasFeedback {
			continue
		}
		strength, occurredAt := engagementPreferenceSignal(state)
		if strength == 0 || (existing.FeedbackResetAt != nil && !occurredAt.After(*existing.FeedbackResetAt)) {
			continue
		}
		candidate, ok := candidates[state.CandidateID]
		if !ok {
			continue
		}
		weight := strength * feedbackDecay(occurredAt, now)
		for _, topic := range parseStringList(candidate.Topics) {
			topicWeights[topic] += weight
		}
		for _, style := range []string{candidate.ContentStyle, candidate.ContentType} {
			if style = strings.TrimSpace(style); style != "" {
				styleWeights[style] += weight * 0.4
			}
		}
		if vector := embeddingVectors[state.CandidateID]; len(vector) > 0 {
			positiveVectors = append(positiveVectors, vector)
			positiveVectorWeights = append(positiveVectorWeights, weight)
		}
		depthPreference += weight * 0.04
	}
	depthPreference = clampScore(depthPreference)
	topicJSON, _ := json.Marshal(topicWeights)
	sourceJSON, _ := json.Marshal(sourceWeights)
	styleJSON, _ := json.Marshal(styleWeights)
	positiveEmbedding := marshalVector(weightedAverageVectors(positiveVectors, positiveVectorWeights))
	negativeEmbedding := marshalVector(weightedAverageVectors(negativeVectors, negativeVectorWeights))

	version := uint(1)
	if existingResult.RowsAffected > 0 {
		version = existing.ProfileVersion
	}
	profile := UserRecommendationProfile{
		UserID:            userID,
		PositiveEmbedding: positiveEmbedding,
		NegativeEmbedding: negativeEmbedding,
		TopicWeights:      string(topicJSON),
		SourceWeights:     string(sourceJSON),
		StyleWeights:      string(styleJSON),
		DepthPreference:   depthPreference,
		ExplorationRate:   settings.ExplorationRate,
		ProfileVersion:    version,
		FeedbackResetAt:   existing.FeedbackResetAt,
	}
	if existingResult.RowsAffected > 0 && !sameRecommendationProfile(existing, profile) {
		profile.ProfileVersion++
	}
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"positive_embedding": profile.PositiveEmbedding,
			"negative_embedding": profile.NegativeEmbedding,
			"topic_weights":      profile.TopicWeights,
			"source_weights":     profile.SourceWeights,
			"style_weights":      profile.StyleWeights,
			"depth_preference":   profile.DepthPreference,
			"exploration_rate":   profile.ExplorationRate,
			"profile_version":    profile.ProfileVersion,
			"feedback_reset_at":  profile.FeedbackResetAt,
			"updated_at":         time.Now(),
		}),
	}).Create(&profile).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

func engagementPreferenceSignal(state discovery.UserCandidateState) (float64, time.Time) {
	if state.ArchivedAt != nil && (state.DeepReadAt == nil || state.ArchivedAt.After(*state.DeepReadAt)) {
		return 1.2, *state.ArchivedAt
	}
	if state.DeepReadAt != nil {
		return 1.2, *state.DeepReadAt
	}
	if state.OpenedAt != nil {
		return 0.15, *state.OpenedAt
	}
	return 0, time.Time{}
}
func feedbackDeltas(action string) (float64, float64, float64, float64) {
	switch normalizeFeedbackAction(action) {
	case RecommendationFeedbackValuable:
		return 1.0, 0, 0.4, 0.03
	case RecommendationFeedbackDeepRead:
		return 1.8, 0, 0.8, 0.18
	case RecommendationFeedbackNotInterested:
		return -0.8, 0, -0.4, -0.03
	case RecommendationFeedbackDuplicate:
		return 0, 0, 0, 0
	case RecommendationFeedbackLowValue:
		return 0, 0, 0, 0
	case RecommendationFeedbackReduceTopic:
		return -1, 0, 0, 0
	case RecommendationFeedbackReduceStyle:
		return 0, 0, -1, 0
	default:
		return 0, 0, 0, 0
	}
}

func feedbackDecay(createdAt time.Time, now time.Time) float64 {
	if createdAt.IsZero() || !now.After(createdAt) {
		return 1
	}
	const halfLife = 90 * 24 * time.Hour
	// Daily buckets make rebuilding deterministic within a digest day while
	// retaining a smooth, auditable half-life across future days.
	elapsedDays := math.Floor(now.Sub(createdAt).Hours() / 24)
	return math.Pow(0.5, elapsedDays/(halfLife.Hours()/24))
}

func weightedAverageVectors(vectors [][]float32, weights []float64) []float32 {
	if len(vectors) == 0 || len(vectors) != len(weights) || len(vectors[0]) == 0 {
		return []float32{}
	}
	dimension := len(vectors[0])
	sum := make([]float64, dimension)
	total := 0.0
	for index, vector := range vectors {
		if len(vector) != dimension || weights[index] <= 0 {
			continue
		}
		for position, value := range vector {
			sum[position] += float64(value) * weights[index]
		}
		total += weights[index]
	}
	if total == 0 {
		return []float32{}
	}
	result := make([]float32, dimension)
	for index := range sum {
		result[index] = float32(sum[index] / total)
	}
	return result
}

func sameRecommendationProfile(left UserRecommendationProfile, right UserRecommendationProfile) bool {
	return left.PositiveEmbedding == right.PositiveEmbedding &&
		left.NegativeEmbedding == right.NegativeEmbedding &&
		left.TopicWeights == right.TopicWeights &&
		left.SourceWeights == right.SourceWeights &&
		left.StyleWeights == right.StyleWeights &&
		left.DepthPreference == right.DepthPreference &&
		left.ExplorationRate == right.ExplorationRate
}

// ResetUserRecommendationPreferences 清空偏好并提升画像版本。
func ResetUserRecommendationPreferences(userID uint) (*UserRecommendationProfile, error) {
	if db == nil || userID == 0 {
		return &UserRecommendationProfile{UserID: userID, ProfileVersion: 1}, nil
	}
	now := recommendationClock.Now()
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return nil, err
	}
	if err := db.Model(&RecommendationSettings{}).Where("user_id = ?", userID).Updates(map[string]interface{}{
		"preferred_topics": "[]", "preferred_languages": "[]", "preferred_length": "", "preferred_depth": 0,
		"favorite_sources": "[]", "updated_at": now,
	}).Error; err != nil {
		return nil, err
	}
	var existing UserRecommendationProfile
	result := db.Where("user_id = ?", userID).Limit(1).Find(&existing)
	if result.Error != nil {
		return nil, result.Error
	}
	version := uint(1)
	if result.RowsAffected > 0 {
		version = existing.ProfileVersion + 1
	}
	profile := UserRecommendationProfile{
		UserID: userID, PositiveEmbedding: "[]", NegativeEmbedding: "[]", TopicWeights: "{}",
		SourceWeights: "{}", StyleWeights: "{}", DepthPreference: 0.5,
		ExplorationRate: settings.ExplorationRate,
		ProfileVersion:  version, FeedbackResetAt: &now,
	}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{
		"positive_embedding", "negative_embedding", "topic_weights", "source_weights", "style_weights", "depth_preference",
		"exploration_rate", "profile_version", "feedback_reset_at", "updated_at",
	})}).Create(&profile).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}
