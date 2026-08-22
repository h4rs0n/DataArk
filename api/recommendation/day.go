package recommendation

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// day.go 负责推荐日报的读写、条目挂载与日期规范化。

// GetRecommendationDaySnapshot 读取指定日期的日报快照及条目。
func GetRecommendationDaySnapshot(userID uint, date string) (*RecommendationDaySnapshot, error) {
	var err error
	date, err = normalizeRecommendationDateForUser(userID, date, recommendationClock.Now())
	if err != nil {
		return nil, err
	}
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
	if err := attachRecommendationItemCandidates(items, day.Status); err != nil {
		return nil, err
	}
	day.RecommendationDate = normalizeRecommendationDate(day.RecommendationDate)
	day.Items = items
	return &RecommendationDaySnapshot{Day: &day, Items: items}, nil
}

// ListRecommendationDays 分页列出用户的推荐日报。
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
	if err := query.Order("recommendation_date desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&days).Error; err != nil {
		return days, err
	}
	for index := range days {
		days[index].RecommendationDate = normalizeRecommendationDate(days[index].RecommendationDate)
	}
	return days, nil
}

// CreateRecommendationDay 创建或复用指定日期的草稿日报。
func CreateRecommendationDay(userID uint, date string, requestedCount int) (*RecommendationDay, error) {
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return nil, err
	}
	date, err = normalizeRecommendationDateForSettings(*settings, date, recommendationClock.Now())
	if err != nil {
		return nil, err
	}
	if requestedCount <= 0 {
		requestedCount = settings.DailyLimit
	}
	day := RecommendationDay{
		UserID:             userID,
		RecommendationDate: date,
		Timezone:           settings.Timezone,
		Status:             RecommendationDayStatusDraft,
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

// AddRecommendationItem 向未发布日报追加一条推荐条目。
func AddRecommendationItem(item *RecommendationItem) (*RecommendationItem, error) {
	if item == nil {
		return nil, errors.New("missing recommendation item")
	}
	if db == nil {
		return item, nil
	}
	if item.UserID == 0 || item.DayID == nil || *item.DayID == 0 || item.FeedBatchID != nil || item.CandidateID == 0 {
		return nil, errors.New("missing recommendation item identity")
	}
	var day RecommendationDay
	if err := db.Select("id", "status").Where("id = ? AND user_id = ?", item.DayID, item.UserID).First(&day).Error; err != nil {
		return nil, err
	}
	if day.Status == RecommendationDayStatusPublished || day.Status == RecommendationDayStatusSupplemented {
		return nil, ErrRecommendationDayImmutable
	}
	var duplicate RecommendationItem
	result := db.Where("day_id = ? AND user_id = ? AND candidate_id = ?", item.DayID, item.UserID, item.CandidateID).Limit(1).Find(&duplicate)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected > 0 {
		return nil, ErrDuplicateRecommendationItem
	}
	if strings.TrimSpace(item.DedupeKey) != "" {
		result = db.Where("day_id = ? AND user_id = ? AND dedupe_key = ?", item.DayID, item.UserID, strings.TrimSpace(item.DedupeKey)).Limit(1).Find(&duplicate)
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
func markRecommendationDayFailed(dayID uint, cause error) error {
	if db == nil || dayID == 0 {
		return nil
	}
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	return db.Model(&RecommendationDay{}).Where("id = ? AND status IN ?", dayID, []string{RecommendationDayStatusDraft, RecommendationDayStatusFailed, "pending"}).Updates(map[string]interface{}{
		"status":         RecommendationDayStatusFailed,
		"failure_reason": truncateError(message, 1000),
		"updated_at":     recommendationClock.Now(),
	}).Error
}

func attachRecommendationItemCandidates(items []RecommendationItem, dayStatus string) error {
	if db == nil || len(items) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.CandidateID)
	}
	var candidates []DiscoveryCandidate
	if err := db.Where("id IN ?", ids).Find(&candidates).Error; err != nil {
		return err
	}
	byID := make(map[uint]DiscoveryCandidate, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.ID] = candidate
	}
	for index := range items {
		live := byID[items[index].CandidateID]
		if dayStatus == RecommendationDayStatusPublished || dayStatus == RecommendationDayStatusSupplemented || items[index].SnapshotURL != "" {
			items[index].Candidate = DiscoveryCandidate{
				ID: items[index].CandidateID, URL: items[index].SnapshotURL, Title: items[index].SnapshotTitle,
				Summary: items[index].SnapshotSummary, Author: items[index].SnapshotAuthor,
				SourceName: items[index].SnapshotSource, PublishedAt: items[index].SnapshotPublishedAt,
				Topics: items[index].SnapshotTopics, ContentType: items[index].SnapshotContentType,
				ContentStyle: items[index].SnapshotStyle, Language: items[index].SnapshotLanguage,
				WordCount: items[index].SnapshotWordCount,
			}
			continue
		}
		items[index].Candidate = live
	}
	return nil
}

func countRecommendationDayItems(dayID uint, userID uint) int {
	if db == nil || dayID == 0 || userID == 0 {
		return 0
	}
	var count int64
	if err := db.Model(&RecommendationItem{}).Where("day_id = ? AND user_id = ?", dayID, userID).Count(&count).Error; err != nil {
		return 0
	}
	return int(count)
}
func normalizeRecommendationDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().Format("2006-01-02")
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Format("2006-01-02")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Format("2006-01-02")
	}
	if len(value) >= len("2006-01-02") {
		if parsed, err := time.Parse("2006-01-02", value[:len("2006-01-02")]); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return value
}

func normalizeRecommendationDateForUser(userID uint, value string, now time.Time) (string, error) {
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return "", err
	}
	return normalizeRecommendationDateForSettings(*settings, value, now)
}

func normalizeRecommendationDateForSettings(settings RecommendationSettings, value string, now time.Time) (string, error) {
	if strings.TrimSpace(value) == "" {
		return recommendationDateForSettings(settings, now), nil
	}
	return normalizeRecommendationDate(value), nil
}

func missingRecommendationDay(userID uint, date string) *RecommendationDay {
	defaults := DefaultRecommendationSettings(userID)
	if settings, err := GetRecommendationSettings(userID); err == nil && settings != nil {
		defaults = *settings
	}
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
