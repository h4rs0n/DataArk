package common

import (
	"DataArk/discovery"
	"DataArk/recommendation"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RecommendationDayStatusMissing   = "missing"
	RecommendationDayStatusPending   = "pending"
	RecommendationDayStatusGenerated = "generated"
	RecommendationDayStatusFailed    = "failed"

	RecommendationFeedbackValuable      = "valuable"
	RecommendationFeedbackNotInterested = "not_interested"
	RecommendationFeedbackDuplicate     = "duplicate"
	RecommendationFeedbackDeepRead      = "deep_read"
	RecommendationFeedbackBlock         = "block"

	UserBlockRuleTopic  = "topic"
	UserBlockRuleSource = "source"
	UserBlockRuleStyle  = "style"

	RecommendationEnrichmentStatusPending = "pending"
	RecommendationEnrichmentStatusReady   = "ready"
	RecommendationEnrichmentStatusFailed  = "failed"
)

var (
	ErrInvalidRecommendationFeedback = errors.New("invalid recommendation feedback action")
	ErrInvalidBlockRule              = errors.New("invalid block rule")
	ErrDuplicateRecommendationItem   = errors.New("recommendation item already exists for this user")
)

type RecommendationDaySnapshot struct {
	Day   *RecommendationDay   `json:"day"`
	Items []RecommendationItem `json:"items"`
}

type RecommendationBlockTarget struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func DefaultRecommendationSettings(userID uint) RecommendationSettings {
	dailyLimit := RECOMMENDATIONDAILYLIMIT
	if dailyLimit <= 0 {
		dailyLimit = 10
	}
	candidateWindowDays := RECOMMENDATIONCANDIDATEWINDOWDAYS
	if candidateWindowDays <= 0 {
		candidateWindowDays = 30
	}
	explorationRate := RECOMMENDATIONEXPLORATIONRATE
	if explorationRate < 0 {
		explorationRate = 0
	}
	if explorationRate > 1 {
		explorationRate = 1
	}
	timezone := strings.TrimSpace(RECOMMENDATIONTIMEZONE)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	generationTime := strings.TrimSpace(RECOMMENDATIONGENERATIONTIME)
	if generationTime == "" {
		generationTime = "07:00"
	}
	return RecommendationSettings{
		UserID:              userID,
		DailyLimit:          dailyLimit,
		Timezone:            timezone,
		GenerationTime:      generationTime,
		CandidateWindowDays: candidateWindowDays,
		ExplorationRate:     explorationRate,
		Enabled:             RECOMMENDATIONENABLED,
	}
}

func GetRecommendationSettings(userID uint) (*RecommendationSettings, error) {
	if db == nil || userID == 0 {
		settings := DefaultRecommendationSettings(userID)
		return &settings, nil
	}

	var settings RecommendationSettings
	result := db.Where("user_id = ?", userID).Limit(1).Find(&settings)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		settings = DefaultRecommendationSettings(userID)
		if err := db.Create(&settings).Error; err != nil {
			return nil, err
		}
	}
	return &settings, nil
}

func SaveRecommendationSettings(settings *RecommendationSettings) (*RecommendationSettings, error) {
	if settings == nil {
		return nil, errors.New("missing recommendation settings")
	}
	if settings.UserID == 0 {
		return nil, errors.New("missing user id")
	}
	normalized := normalizeRecommendationSettings(*settings)
	if db == nil {
		return &normalized, nil
	}
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"daily_limit":           normalized.DailyLimit,
			"timezone":              normalized.Timezone,
			"generation_time":       normalized.GenerationTime,
			"candidate_window_days": normalized.CandidateWindowDays,
			"exploration_rate":      normalized.ExplorationRate,
			"enabled":               normalized.Enabled,
			"updated_at":            time.Now(),
		}),
	}).Create(&normalized).Error; err != nil {
		return nil, err
	}
	return GetRecommendationSettings(settings.UserID)
}

func GetRecommendationDaySnapshot(userID uint, date string) (*RecommendationDaySnapshot, error) {
	date = normalizeRecommendationDate(date)
	if db == nil || userID == 0 {
		return &RecommendationDaySnapshot{Day: missingRecommendationDay(userID, date), Items: []RecommendationItem{}}, nil
	}

	var day RecommendationDay
	result := db.Where("user_id = ? AND recommendation_date = ?", userID, date).Limit(1).Find(&day)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return &RecommendationDaySnapshot{Day: missingRecommendationDay(userID, date), Items: []RecommendationItem{}}, nil
	}
	items := make([]RecommendationItem, 0)
	if err := db.Where("day_id = ? AND user_id = ?", day.ID, userID).Order("rank asc").Find(&items).Error; err != nil {
		return nil, err
	}
	day.Items = items
	return &RecommendationDaySnapshot{Day: &day, Items: items}, nil
}

func ListRecommendationDays(userID uint, from string, to string, page int, pageSize int) ([]RecommendationDay, error) {
	days := make([]RecommendationDay, 0)
	if db == nil || userID == 0 {
		return days, nil
	}
	page, pageSize = normalizePage(page, pageSize)
	query := db.Where("user_id = ?", userID)
	if strings.TrimSpace(from) != "" {
		query = query.Where("recommendation_date >= ?", normalizeRecommendationDate(from))
	}
	if strings.TrimSpace(to) != "" {
		query = query.Where("recommendation_date <= ?", normalizeRecommendationDate(to))
	}
	err := query.Order("recommendation_date desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&days).Error
	return days, err
}

func CreateRecommendationDay(userID uint, date string, requestedCount int) (*RecommendationDay, error) {
	date = normalizeRecommendationDate(date)
	if requestedCount <= 0 {
		requestedCount = DefaultRecommendationSettings(userID).DailyLimit
	}
	day := RecommendationDay{
		UserID:             userID,
		RecommendationDate: date,
		Timezone:           DefaultRecommendationSettings(userID).Timezone,
		Status:             RecommendationDayStatusPending,
		RequestedCount:     requestedCount,
		ActualCount:        0,
	}
	if db == nil || userID == 0 {
		return &day, nil
	}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "recommendation_date"}},
		DoNothing: true,
	}).Create(&day).Error; err != nil {
		return nil, err
	}
	return getRecommendationDay(userID, date)
}

func AddRecommendationItem(item *RecommendationItem) (*RecommendationItem, error) {
	if item == nil {
		return nil, errors.New("missing recommendation item")
	}
	if db == nil {
		return item, nil
	}
	if item.UserID == 0 || item.DayID == 0 || item.CandidateID == 0 {
		return nil, errors.New("missing recommendation item identity")
	}
	var duplicate RecommendationItem
	result := db.Where("user_id = ? AND candidate_id = ?", item.UserID, item.CandidateID).Limit(1).Find(&duplicate)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected > 0 {
		return nil, ErrDuplicateRecommendationItem
	}
	if strings.TrimSpace(item.DedupeKey) != "" {
		result = db.Where("user_id = ? AND dedupe_key = ?", item.UserID, strings.TrimSpace(item.DedupeKey)).Limit(1).Find(&duplicate)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected > 0 {
			return nil, ErrDuplicateRecommendationItem
		}
	}
	if err := db.Create(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func RecordRecommendationFeedback(userID uint, recommendationItemID uint, action string, targets []RecommendationBlockTarget) (*RecommendationFeedback, []UserBlockRule, error) {
	action = normalizeFeedbackAction(action)
	if action == "" {
		return nil, nil, ErrInvalidRecommendationFeedback
	}
	if userID == 0 || recommendationItemID == 0 {
		return nil, nil, errors.New("missing feedback identity")
	}
	if db == nil {
		return &RecommendationFeedback{UserID: userID, RecommendationItemID: recommendationItemID, Action: action}, []UserBlockRule{}, nil
	}

	var item RecommendationItem
	if err := db.First(&item, "id = ? AND user_id = ?", recommendationItemID, userID).Error; err != nil {
		return nil, nil, err
	}
	metadataBytes, _ := json.Marshal(struct {
		BlockTargets []RecommendationBlockTarget `json:"blockTargets,omitempty"`
	}{BlockTargets: targets})

	var feedback RecommendationFeedback
	blockRules := make([]UserBlockRule, 0)
	err := db.Transaction(func(tx *gorm.DB) error {
		feedback = RecommendationFeedback{
			UserID:               userID,
			RecommendationItemID: recommendationItemID,
			CandidateID:          item.CandidateID,
			Action:               action,
			Metadata:             string(metadataBytes),
		}
		if err := tx.Create(&feedback).Error; err != nil {
			return err
		}
		if action != RecommendationFeedbackBlock {
			return nil
		}
		for _, target := range targets {
			rule, err := buildBlockRule(userID, target)
			if err != nil {
				return err
			}
			if err := tx.Create(&rule).Error; err != nil {
				return err
			}
			blockRules = append(blockRules, rule)
		}
		if len(blockRules) == 0 {
			return ErrInvalidBlockRule
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &feedback, blockRules, nil
}

func RevertRecommendationFeedback(userID uint, recommendationItemID uint) error {
	if db == nil || userID == 0 || recommendationItemID == 0 {
		return nil
	}
	now := time.Now()
	return db.Model(&RecommendationFeedback{}).
		Where("user_id = ? AND recommendation_item_id = ? AND reverted_at IS NULL", userID, recommendationItemID).
		Update("reverted_at", &now).Error
}

func ListUserBlockRules(userID uint, activeOnly bool) ([]UserBlockRule, error) {
	rules := make([]UserBlockRule, 0)
	if db == nil || userID == 0 {
		return rules, nil
	}
	query := db.Where("user_id = ?", userID).Order("created_at desc")
	if activeOnly {
		query = query.Where("active = ?", true)
	}
	err := query.Find(&rules).Error
	return rules, err
}

func DeleteUserBlockRule(userID uint, ruleID uint) error {
	if db == nil || userID == 0 || ruleID == 0 {
		return nil
	}
	return db.Model(&UserBlockRule{}).Where("id = ? AND user_id = ?", ruleID, userID).Updates(map[string]interface{}{
		"active":     false,
		"updated_at": time.Now(),
	}).Error
}

func EnrichDiscoveryCandidate(ctx context.Context, candidateID uint, provider recommendation.EnrichmentProvider) (*DiscoveryCandidate, error) {
	if provider == nil {
		return nil, errors.New("missing enrichment provider")
	}
	if db == nil || candidateID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return nil, err
	}
	input := recommendation.EnrichmentInput{
		CandidateID: candidate.ID,
		URL:         candidate.URL,
		Title:       candidate.Title,
		Summary:     candidate.Summary,
		BodyText:    candidate.BodyText,
		PublishedAt: candidate.PublishedAt,
	}
	result, err := provider.Enrich(ctx, input)
	if err != nil {
		_ = db.Model(&candidate).Updates(map[string]interface{}{
			"enrichment_status": RecommendationEnrichmentStatusFailed,
			"enrichment_error":  err.Error(),
			"updated_at":        time.Now(),
		}).Error
		return nil, err
	}

	bodyForHash := candidate.BodyText
	if strings.TrimSpace(bodyForHash) == "" {
		bodyForHash = candidate.Summary
	}
	contentHash := candidate.ContentHash
	if strings.TrimSpace(bodyForHash) != "" {
		contentHash = discovery.ContentHash(bodyForHash)
	}
	normalizedURL := candidate.NormalizedURL
	if normalizedURL == "" {
		if value, err := discovery.NormalizeArticleURL(candidate.URL); err == nil {
			normalizedURL = value
		}
	}
	canonicalURL := candidate.CanonicalURL
	if canonicalURL == "" {
		canonicalURL = normalizedURL
	}
	dedupeKey := normalizedURL
	if contentHash != "" {
		dedupeKey = contentHash
	}
	topics, _ := json.Marshal(result.Topics)
	entities, _ := json.Marshal(result.Entities)

	updates := map[string]interface{}{
		"summary":           firstNonEmpty(result.Summary, candidate.Summary),
		"normalized_url":    normalizedURL,
		"canonical_url":     canonicalURL,
		"content_hash":      contentHash,
		"dedupe_key":        dedupeKey,
		"topics":            string(topics),
		"entities":          string(entities),
		"content_type":      strings.TrimSpace(result.ContentType),
		"content_style":     strings.TrimSpace(result.ContentStyle),
		"language":          strings.TrimSpace(result.Language),
		"quality_score":     clampScore(result.QualityScore),
		"depth_score":       clampScore(result.DepthScore),
		"llm_model":         strings.TrimSpace(result.Model),
		"prompt_version":    strings.TrimSpace(result.PromptVersion),
		"enrichment_status": RecommendationEnrichmentStatusReady,
		"enrichment_error":  "",
		"enriched_at":       time.Now(),
		"updated_at":        time.Now(),
	}
	if err := db.Model(&candidate).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return nil, err
	}
	return &candidate, nil
}

func normalizeRecommendationSettings(settings RecommendationSettings) RecommendationSettings {
	defaults := DefaultRecommendationSettings(settings.UserID)
	if settings.DailyLimit <= 0 {
		settings.DailyLimit = defaults.DailyLimit
	}
	if settings.CandidateWindowDays <= 0 {
		settings.CandidateWindowDays = defaults.CandidateWindowDays
	}
	if strings.TrimSpace(settings.Timezone) == "" {
		settings.Timezone = defaults.Timezone
	}
	if strings.TrimSpace(settings.GenerationTime) == "" {
		settings.GenerationTime = defaults.GenerationTime
	}
	if settings.ExplorationRate < 0 {
		settings.ExplorationRate = 0
	}
	if settings.ExplorationRate > 1 {
		settings.ExplorationRate = 1
	}
	return settings
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func normalizeRecommendationDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().Format("2006-01-02")
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Format("2006-01-02")
	}
	return value
}

func missingRecommendationDay(userID uint, date string) *RecommendationDay {
	defaults := DefaultRecommendationSettings(userID)
	return &RecommendationDay{
		UserID:             userID,
		RecommendationDate: date,
		Timezone:           defaults.Timezone,
		Status:             RecommendationDayStatusMissing,
		RequestedCount:     defaults.DailyLimit,
		ActualCount:        0,
		Items:              []RecommendationItem{},
	}
}

func getRecommendationDay(userID uint, date string) (*RecommendationDay, error) {
	var day RecommendationDay
	if err := db.Where("user_id = ? AND recommendation_date = ?", userID, normalizeRecommendationDate(date)).Limit(1).Find(&day).Error; err != nil {
		return nil, err
	}
	return &day, nil
}

func normalizeFeedbackAction(action string) string {
	switch strings.TrimSpace(action) {
	case RecommendationFeedbackValuable:
		return RecommendationFeedbackValuable
	case RecommendationFeedbackNotInterested:
		return RecommendationFeedbackNotInterested
	case RecommendationFeedbackDuplicate:
		return RecommendationFeedbackDuplicate
	case RecommendationFeedbackDeepRead:
		return RecommendationFeedbackDeepRead
	case RecommendationFeedbackBlock:
		return RecommendationFeedbackBlock
	default:
		return ""
	}
}

func buildBlockRule(userID uint, target RecommendationBlockTarget) (UserBlockRule, error) {
	ruleType := strings.TrimSpace(target.Type)
	ruleValue := strings.TrimSpace(target.Value)
	switch ruleType {
	case UserBlockRuleTopic, UserBlockRuleSource, UserBlockRuleStyle:
	default:
		return UserBlockRule{}, ErrInvalidBlockRule
	}
	if ruleValue == "" {
		return UserBlockRule{}, ErrInvalidBlockRule
	}
	return UserBlockRule{UserID: userID, RuleType: ruleType, RuleValue: ruleValue, Active: true}, nil
}

func normalizePage(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
