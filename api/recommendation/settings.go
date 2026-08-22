package recommendation

import (
	"DataArk/config"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// settings.go 负责推荐设置的默认值、读写与规范化。

// DefaultRecommendationSettings 根据全局配置生成用户推荐设置默认值。
func DefaultRecommendationSettings(userID uint) RecommendationSettings {
	dailyLimit := config.RECOMMENDATIONDAILYLIMIT
	if dailyLimit <= 0 {
		dailyLimit = 10
	}
	candidateWindowDays := config.RECOMMENDATIONCANDIDATEWINDOWDAYS
	if candidateWindowDays <= 0 {
		candidateWindowDays = 30
	}
	explorationRate := config.RECOMMENDATIONEXPLORATIONRATE
	if explorationRate < 0 {
		explorationRate = 0
	}
	if explorationRate > 1 {
		explorationRate = 1
	}
	timezone := strings.TrimSpace(config.RECOMMENDATIONTIMEZONE)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	generationTime := strings.TrimSpace(config.RECOMMENDATIONGENERATIONTIME)
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
		Enabled:             config.RECOMMENDATIONENABLED,
	}
}

// GetRecommendationSettings 读取用户推荐设置，不存在时写入默认值。
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

// SaveRecommendationSettings 规范化并持久化用户推荐设置。
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
			"preferred_topics":      normalized.PreferredTopics,
			"preferred_languages":   normalized.PreferredLanguages,
			"preferred_length":      normalized.PreferredLength,
			"preferred_depth":       normalized.PreferredDepth,
			"favorite_sources":      normalized.FavoriteSources,
			"enabled":               normalized.Enabled,
			"updated_at":            time.Now(),
		}),
	}).Create(&normalized).Error; err != nil {
		return nil, err
	}
	return GetRecommendationSettings(settings.UserID)
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
	if _, err := time.LoadLocation(settings.Timezone); err != nil {
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
	settings.PreferredTopics = normalizeJSONList(settings.PreferredTopics)
	settings.PreferredLanguages = normalizeJSONList(settings.PreferredLanguages)
	settings.FavoriteSources = normalizeJSONList(settings.FavoriteSources)
	settings.PreferredLength = strings.ToLower(strings.TrimSpace(settings.PreferredLength))
	switch settings.PreferredLength {
	case "", "short", "long":
	default:
		settings.PreferredLength = ""
	}
	settings.PreferredDepth = clampScore(settings.PreferredDepth)
	return settings
}

func normalizeJSONList(raw string) string {
	values := parseStringList(raw)
	sort.Slice(values, func(i, j int) bool { return strings.ToLower(values[i]) < strings.ToLower(values[j]) })
	encoded, _ := json.Marshal(values)
	return string(encoded)
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
