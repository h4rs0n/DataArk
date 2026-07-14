package recommendation

import (
	"DataArk/config"
	"DataArk/discovery"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RecommendationDayStatusMissing      = "missing"
	RecommendationDayStatusDraft        = "draft"
	RecommendationDayStatusPublished    = "published"
	RecommendationDayStatusSupplemented = "supplemented"
	RecommendationDayStatusFailed       = "failed"

	// Compatibility names remain source-compatible while persisted lifecycle
	// values use the immutable v3 terminology.
	RecommendationDayStatusPending   = RecommendationDayStatusDraft
	RecommendationDayStatusGenerated = RecommendationDayStatusPublished

	RecommendationFeedbackValuable      = "valuable"
	RecommendationFeedbackNotInterested = "not_interested"
	RecommendationFeedbackDuplicate     = "duplicate"
	RecommendationFeedbackTooRepetitive = "too_repetitive"
	RecommendationFeedbackDeepRead      = "deep_read"
	RecommendationFeedbackBlock         = "block"
	RecommendationFeedbackBlockSource   = "block_source"
	RecommendationFeedbackReduceTopic   = "reduce_topic"
	RecommendationFeedbackReduceStyle   = "reduce_style"

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
	ErrRecommendationDayImmutable    = errors.New("published recommendation day is immutable")
)

type RecommendationDaySnapshot struct {
	Day   *RecommendationDay   `json:"day"`
	Items []RecommendationItem `json:"items"`
}

type RecommendationBlockTarget struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type recommendationFeedbackMetadata struct {
	BlockTargets []RecommendationBlockTarget `json:"blockTargets,omitempty"`
}

type recommendationCandidateScore struct {
	Candidate         DiscoveryCandidate
	Topics            []string
	SourceHost        string
	Author            string
	PoolTags          []string
	PoolType          string
	Exploration       bool
	ExplorationReason string
	ContentUpdated    bool
	CooldownRepeat    bool
	RetrievalScore    float64
	RerankScore       float64
	RerankRank        int
	FinalScore        float64
	Reason            string
}

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

func ConfiguredEnrichmentProvider() EnrichmentProvider {
	if strings.TrimSpace(config.LLMCHATMODEL) == "" {
		return RuleBasedEnrichmentProvider{}
	}
	return configuredOpenAICompatibleProvider()
}

func ConfiguredRecommendationReranker() RerankProvider {
	if strings.TrimSpace(config.LLMCHATMODEL) == "" {
		return nil
	}
	return configuredOpenAICompatibleProvider()
}

func configuredOpenAICompatibleProvider() OpenAICompatibleProvider {
	timeout, err := time.ParseDuration(strings.TrimSpace(config.LLMTIMEOUT))
	if err != nil || timeout <= 0 {
		timeout = 30 * time.Second
	}
	return OpenAICompatibleProvider{
		BaseURL:        config.LLMBASEURL,
		APIKey:         config.LLMAPIKEY,
		ChatModel:      config.LLMCHATMODEL,
		EmbeddingModel: config.LLMEMBEDDINGMODEL,
		Timeout:        timeout,
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

func GenerateDailyRecommendations(ctx context.Context, userID uint, date string) (*RecommendationDaySnapshot, error) {
	return GenerateDailyRecommendationsWithReranker(ctx, userID, date, ConfiguredRecommendationReranker())
}

func GenerateDailyRecommendationsWithReranker(ctx context.Context, userID uint, date string, reranker RerankProvider) (*RecommendationDaySnapshot, error) {
	return generateDailyRecommendationsWithOptions(ctx, userID, date, reranker, false)
}

func RegenerateDailyRecommendations(ctx context.Context, userID uint, date string) (*RecommendationDaySnapshot, error) {
	// Kept for source compatibility: retries are now non-destructive and an
	// already published digest is returned byte-semantically unchanged.
	return generateDailyRecommendationsWithOptions(ctx, userID, date, ConfiguredRecommendationReranker(), true)
}

func generateDailyRecommendationsWithOptions(ctx context.Context, userID uint, date string, reranker RerankProvider, force bool) (*RecommendationDaySnapshot, error) {
	if db == nil || userID == 0 {
		if userID != 0 {
			if localDate, err := normalizeRecommendationDateForUser(userID, date, recommendationClock.Now()); err == nil {
				date = localDate
			}
		}
		return &RecommendationDaySnapshot{Day: missingRecommendationDay(userID, normalizeRecommendationDate(date)), Items: []RecommendationItem{}}, nil
	}
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return nil, err
	}
	date, err = normalizeRecommendationDateForSettings(*settings, date, recommendationClock.Now())
	if err != nil {
		return nil, err
	}
	if !force && !settings.Enabled {
		return GetRecommendationDaySnapshot(userID, date)
	}
	day, err := CreateRecommendationDay(userID, date, settings.DailyLimit)
	if err != nil {
		return nil, err
	}
	existing, err := GetRecommendationDaySnapshot(userID, day.RecommendationDate)
	if err != nil {
		return nil, err
	}
	if existing.Day != nil && (existing.Day.Status == RecommendationDayStatusPublished || existing.Day.Status == RecommendationDayStatusSupplemented) {
		return existing, nil
	}
	profile, err := RebuildUserRecommendationProfile(userID)
	if err != nil {
		return nil, err
	}
	selectionLimit := settings.DailyLimit
	if reranker != nil && config.RECOMMENDATIONRERANKLIMIT > selectionLimit {
		selectionLimit = config.RECOMMENDATIONRERANKLIMIT
	}
	selection, err := selectDailyRecommendationCandidatesV3(ctx, userID, *settings, profile, selectionLimit)
	if err != nil {
		_ = markRecommendationDayFailed(day.ID, err)
		return nil, err
	}
	reranked, rerankModel, rerankPrompt, degradationReason := applyRecommendationReranker(ctx, userID, selectionLimit, selection.Candidates, profile, reranker)
	selected, softRelaxations := diversifyRecommendationCandidatesV3(reranked, settings.DailyLimit, settings.ExplorationRate)
	selection.SoftRelaxations = softRelaxations
	if possible := min(settings.DailyLimit, len(reranked)); len(selected) < possible {
		selection.Excluded["same_cluster_daily"] += possible - len(selected)
	}
	now := recommendationClock.Now()
	items := buildRecommendationItems(day.ID, userID, selected, 1, profile.ProfileVersion, false, now)
	if err := publishRecommendationDay(day.ID, userID, items, map[string]interface{}{
		"status":             RecommendationDayStatusPublished,
		"actual_count":       len(items),
		"shortage_reasons":   marshalSelectionAudit(settings.DailyLimit, len(items), selection, softRelaxations),
		"policy_version":     recommendationSelectionPolicyV3,
		"profile_version":    profile.ProfileVersion,
		"llm_model":          rerankModel,
		"prompt_version":     rerankPrompt,
		"failure_reason":     "",
		"degraded":           degradationReason != "",
		"degradation_reason": degradationReason,
		"generated_at":       &now,
		"published_at":       &now,
		"updated_at":         now,
	}); err != nil {
		_ = markRecommendationDayFailed(day.ID, err)
		return nil, err
	}
	return GetRecommendationDaySnapshot(userID, day.RecommendationDate)
}

func buildRecommendationItems(dayID uint, userID uint, selected []recommendationCandidateScore, firstRank int, profileVersion uint, supplemental bool, now time.Time) []RecommendationItem {
	items := make([]RecommendationItem, 0, len(selected))
	for index, scored := range selected {
		item := RecommendationItem{
			DayID: dayID, UserID: userID, CandidateID: scored.Candidate.ID,
			DedupeKey: strings.TrimSpace(scored.Candidate.DedupeKey), AssessmentID: scored.Candidate.CurrentAssessmentID,
			Rank: firstRank + index, RetrievalScore: scored.RetrievalScore, RerankScore: scored.RerankScore,
			FinalScore: scored.FinalScore, Reason: scored.Reason, ReasonMetadata: buildReasonMetadata(scored),
			SnapshotTitle: scored.Candidate.Title, SnapshotURL: scored.Candidate.URL,
			SnapshotSummary: scored.Candidate.Summary, SnapshotAuthor: scored.Candidate.Author,
			SnapshotSource: scored.Candidate.SourceName, SnapshotPublishedAt: scored.Candidate.PublishedAt,
			PoolType: scored.PoolType, ExplorationReason: scored.ExplorationReason,
			ContentVersion: scored.Candidate.ContentVersion, ContentUpdated: scored.ContentUpdated,
			CooldownRepeat: scored.CooldownRepeat, ProfileVersion: profileVersion,
			Supplemental: supplemental, CreatedAt: now, UpdatedAt: now,
		}
		if supplemental {
			item.SupplementedAt = &now
		}
		items = append(items, item)
	}
	return items
}

func publishRecommendationDay(dayID uint, userID uint, items []RecommendationItem, updates map[string]interface{}) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var day RecommendationDay
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", dayID, userID).First(&day).Error; err != nil {
			return err
		}
		if day.Status == RecommendationDayStatusPublished || day.Status == RecommendationDayStatusSupplemented {
			return nil
		}
		if day.Status != RecommendationDayStatusDraft && day.Status != RecommendationDayStatusFailed && day.Status != "pending" {
			return fmt.Errorf("recommendation day %d cannot publish from status %q", day.ID, day.Status)
		}
		var feedbackCount int64
		if err := tx.Table("recommendation_feedbacks AS feedback").
			Joins("JOIN recommendation_items AS item ON item.id = feedback.recommendation_item_id").
			Where("item.day_id = ? AND item.user_id = ?", dayID, userID).Count(&feedbackCount).Error; err != nil {
			return err
		}
		if feedbackCount > 0 {
			return errors.New("unpublished recommendation items have feedback and require audit repair")
		}
		if err := tx.Where("day_id = ? AND user_id = ?", dayID, userID).Delete(&RecommendationItem{}).Error; err != nil {
			return err
		}
		for index := range items {
			if err := tx.Create(&items[index]).Error; err != nil {
				return err
			}
			if err := discovery.RecordUserCandidateExposure(tx, userID, items[index].CandidateID, items[index].CreatedAt); err != nil {
				return err
			}
		}
		return tx.Model(&RecommendationDay{}).Where("id = ? AND user_id = ?", dayID, userID).Updates(updates).Error
	})
}

func SupplementDailyRecommendations(ctx context.Context, userID uint, date string) (*RecommendationDaySnapshot, error) {
	return SupplementDailyRecommendationsWithReranker(ctx, userID, date, ConfiguredRecommendationReranker())
}

func SupplementDailyRecommendationsWithReranker(ctx context.Context, userID uint, date string, reranker RerankProvider) (*RecommendationDaySnapshot, error) {
	snapshot, err := GetRecommendationDaySnapshot(userID, date)
	if err != nil || snapshot.Day == nil {
		return snapshot, err
	}
	day := snapshot.Day
	if day.Status != RecommendationDayStatusPublished && day.Status != RecommendationDayStatusSupplemented {
		return nil, fmt.Errorf("recommendation day %d is not published", day.ID)
	}
	missing := day.RequestedCount - day.ActualCount
	if missing <= 0 {
		return snapshot, nil
	}
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return nil, err
	}
	profile, err := RebuildUserRecommendationProfile(userID)
	if err != nil {
		return nil, err
	}
	selectionLimit := missing
	if reranker != nil && config.RECOMMENDATIONRERANKLIMIT > selectionLimit {
		selectionLimit = config.RECOMMENDATIONRERANKLIMIT
	}
	selection, err := selectDailyRecommendationCandidatesV3(ctx, userID, *settings, profile, selectionLimit)
	if err != nil {
		return nil, err
	}
	reranked, rerankModel, rerankPrompt, degradationReason := applyRecommendationReranker(ctx, userID, selectionLimit, selection.Candidates, profile, reranker)
	selected, relaxations := diversifyRecommendationCandidatesV3(reranked, missing, settings.ExplorationRate)
	now := recommendationClock.Now()
	items := buildRecommendationItems(day.ID, userID, selected, day.ActualCount+1, profile.ProfileVersion, true, now)
	if err := appendRecommendationSupplement(day.ID, userID, day.RequestedCount, items, selection, relaxations, rerankModel, rerankPrompt, degradationReason, now); err != nil {
		return nil, err
	}
	return GetRecommendationDaySnapshot(userID, day.RecommendationDate)
}

func appendRecommendationSupplement(dayID uint, userID uint, requestedCount int, items []RecommendationItem, selection *recommendationSelectionReport, relaxations []string, rerankModel string, rerankPrompt string, degradationReason string, now time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var day RecommendationDay
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", dayID, userID).First(&day).Error; err != nil {
			return err
		}
		if day.Status != RecommendationDayStatusPublished && day.Status != RecommendationDayStatusSupplemented {
			return fmt.Errorf("recommendation day %d cannot be supplemented from status %q", day.ID, day.Status)
		}
		missing := requestedCount - day.ActualCount
		if missing <= 0 || len(items) == 0 {
			return nil
		}
		if len(items) > missing {
			items = items[:missing]
		}
		var maxRank int
		if err := tx.Model(&RecommendationItem{}).Where("day_id = ? AND user_id = ?", dayID, userID).Select("COALESCE(MAX(rank), 0)").Scan(&maxRank).Error; err != nil {
			return err
		}
		appended := 0
		for index := range items {
			items[index].Rank = maxRank + appended + 1
			var count int64
			query := tx.Model(&RecommendationItem{}).Where("day_id = ? AND user_id = ? AND candidate_id = ?", dayID, userID, items[index].CandidateID)
			if key := strings.TrimSpace(items[index].DedupeKey); key != "" {
				query = tx.Model(&RecommendationItem{}).Where("day_id = ? AND user_id = ? AND (candidate_id = ? OR dedupe_key = ?)", dayID, userID, items[index].CandidateID, key)
			}
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			if err := tx.Create(&items[index]).Error; err != nil {
				return err
			}
			if err := discovery.RecordUserCandidateExposure(tx, userID, items[index].CandidateID, now); err != nil {
				return err
			}
			appended++
		}
		if appended == 0 {
			return nil
		}
		actual := day.ActualCount + appended
		return tx.Model(&RecommendationDay{}).Where("id = ? AND user_id = ?", dayID, userID).Updates(map[string]interface{}{
			"status": RecommendationDayStatusSupplemented, "actual_count": actual,
			"shortage_reasons":  marshalSelectionAudit(requestedCount, actual, selection, relaxations),
			"supplement_policy": "append_missing_v1", "supplemented_at": &now,
			"degraded":           day.Degraded || degradationReason != "",
			"degradation_reason": firstNonEmpty(day.DegradationReason, degradationReason),
			"llm_model":          firstNonEmpty(day.LLMModel, rerankModel), "prompt_version": firstNonEmpty(day.PromptVersion, rerankPrompt),
			"updated_at": now,
		}).Error
	})
}

func RecordRecommendationFeedback(userID uint, recommendationItemID uint, action string, targets []RecommendationBlockTarget) (*RecommendationFeedback, []UserBlockRule, error) {
	rawAction := strings.TrimSpace(action)
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
	targets = normalizeFeedbackTargets(targets)
	if (action == RecommendationFeedbackBlock || action == RecommendationFeedbackReduceTopic || action == RecommendationFeedbackReduceStyle) && len(targets) == 0 {
		return nil, nil, ErrInvalidBlockRule
	}
	for _, target := range targets {
		if (rawAction == RecommendationFeedbackBlockSource && target.Type != UserBlockRuleSource) ||
			(action == RecommendationFeedbackReduceTopic && target.Type != UserBlockRuleTopic) ||
			(action == RecommendationFeedbackReduceStyle && target.Type != UserBlockRuleStyle) {
			return nil, nil, ErrInvalidBlockRule
		}
	}
	metadataBytes, _ := json.Marshal(recommendationFeedbackMetadata{BlockTargets: targets})
	metadata := string(metadataBytes)

	var feedback RecommendationFeedback
	blockRules := make([]UserBlockRule, 0)
	err := db.Transaction(func(tx *gorm.DB) error {
		var current RecommendationFeedback
		currentResult := tx.Where("user_id = ? AND recommendation_item_id = ? AND is_current = ?", userID, recommendationItemID, true).Limit(1).Find(&current)
		if currentResult.Error != nil {
			return currentResult.Error
		}
		if currentResult.RowsAffected > 0 && current.Action == action && current.Metadata == metadata {
			feedback = current
			return tx.Where("feedback_id = ? AND active = ?", current.ID, true).Find(&blockRules).Error
		}
		now := recommendationClock.Now()
		var supersedesID *uint
		if currentResult.RowsAffected > 0 {
			supersedesID = &current.ID
			if err := tx.Model(&RecommendationFeedback{}).Where("id = ?", current.ID).Updates(map[string]interface{}{
				"is_current": false, "current_key": nil, "reverted_at": now, "closed_reason": "superseded",
			}).Error; err != nil {
				return err
			}
			if err := tx.Model(&UserBlockRule{}).Where("feedback_id = ? AND active = ?", current.ID, true).Updates(map[string]interface{}{
				"active": false, "updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		currentKey := fmt.Sprintf("%d:%d", userID, recommendationItemID)
		feedback = RecommendationFeedback{
			UserID:               userID,
			RecommendationItemID: recommendationItemID,
			CandidateID:          item.CandidateID,
			Action:               action,
			Metadata:             metadata,
			IsCurrent:            true,
			CurrentKey:           &currentKey,
			SupersedesID:         supersedesID,
			CreatedAt:            now,
		}
		if err := tx.Create(&feedback).Error; err != nil {
			return err
		}
		if action == RecommendationFeedbackDuplicate {
			signal := discovery.DiscoveryDuplicateReviewSignal{
				CandidateID: item.CandidateID, ReporterUserID: userID,
				RecommendationItemID: recommendationItemID, Status: "pending",
			}
			if err := tx.Create(&signal).Error; err != nil {
				return err
			}
		}
		if err := syncUserCandidateFeedbackState(tx, userID, item.CandidateID, action, now); err != nil {
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
			rule.FeedbackID = &feedback.ID
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
		if isUniqueConstraintError(err) {
			var concurrent RecommendationFeedback
			result := db.Where("user_id = ? AND recommendation_item_id = ? AND is_current = ? AND action = ? AND metadata = ?", userID, recommendationItemID, true, action, metadata).Limit(1).Find(&concurrent)
			if result.Error == nil && result.RowsAffected > 0 {
				if rulesErr := db.Where("feedback_id = ? AND active = ?", concurrent.ID, true).Find(&blockRules).Error; rulesErr == nil {
					return &concurrent, blockRules, nil
				}
			}
		}
		return nil, nil, err
	}
	if action == RecommendationFeedbackValuable || action == RecommendationFeedbackDeepRead {
		_ = discovery.RefreshCandidateSiteOperationalStats(item.CandidateID)
	}
	return &feedback, blockRules, nil
}

func isUniqueConstraintError(err error) bool {
	message := strings.ToLower(err.Error())
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key")
}

func RevertRecommendationFeedback(userID uint, recommendationItemID uint) error {
	if db == nil || userID == 0 || recommendationItemID == 0 {
		return nil
	}
	now := recommendationClock.Now()
	return db.Transaction(func(tx *gorm.DB) error {
		var item RecommendationItem
		if err := tx.Where("id = ? AND user_id = ?", recommendationItemID, userID).First(&item).Error; err != nil {
			return err
		}
		var current RecommendationFeedback
		result := tx.Where("user_id = ? AND recommendation_item_id = ? AND is_current = ?", userID, recommendationItemID, true).Limit(1).Find(&current)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if err := tx.Model(&RecommendationFeedback{}).Where("id = ?", current.ID).Updates(map[string]interface{}{
			"is_current": false, "current_key": nil, "reverted_at": now, "closed_reason": "reverted",
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&UserBlockRule{}).Where("feedback_id = ? AND active = ?", current.ID, true).Updates(map[string]interface{}{
			"active": false, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&discovery.UserCandidateState{}).Where("user_id = ? AND candidate_id = ?", userID, item.CandidateID).Updates(map[string]interface{}{
			"current_feedback": "", "feedback_revoked": now, "updated_at": now,
		}).Error
	})
}

func GetCurrentRecommendationFeedback(userID uint, recommendationItemID uint) (*RecommendationFeedback, error) {
	if db == nil || userID == 0 || recommendationItemID == 0 {
		return nil, nil
	}
	var feedback RecommendationFeedback
	result := db.Where("user_id = ? AND recommendation_item_id = ? AND is_current = ?", userID, recommendationItemID, true).Limit(1).Find(&feedback)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, result.Error
	}
	return &feedback, nil
}

func ListRecommendationFeedbackHistory(userID uint, recommendationItemID uint) ([]RecommendationFeedback, error) {
	history := make([]RecommendationFeedback, 0)
	if db == nil || userID == 0 || recommendationItemID == 0 {
		return history, nil
	}
	err := db.Where("user_id = ? AND recommendation_item_id = ?", userID, recommendationItemID).Order("created_at asc, id asc").Find(&history).Error
	return history, err
}

func normalizeFeedbackTargets(targets []RecommendationBlockTarget) []RecommendationBlockTarget {
	cleaned := make([]RecommendationBlockTarget, 0, len(targets))
	seen := make(map[string]struct{})
	for _, target := range targets {
		target.Type = strings.ToLower(strings.TrimSpace(target.Type))
		target.Value = strings.TrimSpace(target.Value)
		if target.Type == "" || target.Value == "" {
			continue
		}
		key := target.Type + "\x00" + strings.ToLower(target.Value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, target)
	}
	sort.Slice(cleaned, func(i, j int) bool {
		if cleaned[i].Type == cleaned[j].Type {
			return strings.ToLower(cleaned[i].Value) < strings.ToLower(cleaned[j].Value)
		}
		return cleaned[i].Type < cleaned[j].Type
	})
	return cleaned
}

func syncUserCandidateFeedbackState(tx *gorm.DB, userID uint, candidateID uint, action string, now time.Time) error {
	state := discovery.UserCandidateState{UserID: userID, CandidateID: candidateID, CreatedAt: now, UpdatedAt: now}
	if err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "candidate_id"}}, DoNothing: true,
	}).Create(&state).Error; err != nil {
		return err
	}
	updates := map[string]interface{}{
		"current_feedback": action, "feedback_set_at": now, "feedback_revoked": nil, "updated_at": now,
	}
	switch action {
	case RecommendationFeedbackDeepRead:
		updates["opened_at"] = now
		updates["read_at"] = now
		updates["deep_read_at"] = now
	case RecommendationFeedbackValuable, RecommendationFeedbackNotInterested, RecommendationFeedbackDuplicate,
		RecommendationFeedbackBlock, RecommendationFeedbackReduceTopic, RecommendationFeedbackReduceStyle:
	default:
		return nil
	}
	return tx.Model(&discovery.UserCandidateState{}).Where("user_id = ? AND candidate_id = ?", userID, candidateID).Updates(updates).Error
}

func userCandidateStateExcludesRecommendation(state discovery.UserCandidateState) bool {
	return state.OpenedAt != nil || state.ReadAt != nil || state.DeepReadAt != nil || state.ArchivedAt != nil || strings.TrimSpace(state.CurrentFeedback) != ""
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

func EnrichPendingDiscoveryCandidates(ctx context.Context, limit int, provider EnrichmentProvider) (int, error) {
	if db == nil {
		return 0, nil
	}
	if provider == nil {
		provider = ConfiguredEnrichmentProvider()
	}
	if limit <= 0 {
		limit = 50
	}
	var candidates []DiscoveryCandidate
	if err := db.Where("enrichment_status = ? OR enrichment_status = '' OR enrichment_status IS NULL", RecommendationEnrichmentStatusPending).
		Where("processing_state = ? AND eligibility_state = ?", discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible).
		Where("dedupe_state = ? AND (representative_id IS NULL OR representative_id = id)", discovery.DiscoveryDedupeReady).
		Order("last_seen_at desc").
		Limit(limit).
		Find(&candidates).Error; err != nil {
		return 0, err
	}
	enriched := 0
	var firstErr error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return enriched, err
		}
		if _, err := EnrichDiscoveryCandidate(ctx, candidate.ID, provider); err != nil {
			if !isRuleBasedEnrichmentProvider(provider) {
				if _, fallbackErr := EnrichDiscoveryCandidate(ctx, candidate.ID, RuleBasedEnrichmentProvider{}); fallbackErr == nil {
					enriched++
					continue
				}
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		enriched++
	}
	return enriched, firstErr
}

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

func EnrichDiscoveryCandidate(ctx context.Context, candidateID uint, provider EnrichmentProvider) (*DiscoveryCandidate, error) {
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
	input := EnrichmentInput{
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

func selectDailyRecommendationCandidatesLegacy(ctx context.Context, userID uint, settings RecommendationSettings, profile *UserRecommendationProfile, selectionLimit int) ([]recommendationCandidateScore, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if selectionLimit <= 0 {
		selectionLimit = settings.DailyLimit
	}
	if selectionLimit <= 0 {
		selectionLimit = 10
	}
	poolSize := config.RECOMMENDATIONCANDIDATEPOOLSIZE
	if poolSize < selectionLimit {
		poolSize = selectionLimit * 10
	}
	cutoff := time.Now().AddDate(0, 0, -settings.CandidateWindowDays)
	var candidates []DiscoveryCandidate
	query := db.Where("enrichment_status = ?", RecommendationEnrichmentStatusReady).
		Where("processing_state = ? AND eligibility_state = ?", discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible).
		Where("dedupe_state = ? AND (representative_id IS NULL OR representative_id = id)", discovery.DiscoveryDedupeReady).
		Where("status <> ?", DiscoveryCandidateStatusIgnored).
		Where("(published_at IS NULL OR published_at >= ?)", cutoff).
		Order("quality_score desc, depth_score desc, score desc, last_seen_at desc").
		Limit(poolSize * 4)
	if err := query.Find(&candidates).Error; err != nil {
		return nil, err
	}
	vectorCandidateIDs, err := loadPGVectorCandidateIDs(ctx, profile, poolSize)
	if err != nil {
		return nil, err
	}
	vectorBoosts := make(map[uint]float64)
	if len(vectorCandidateIDs) > 0 {
		known := make(map[uint]struct{}, len(candidates))
		for _, candidate := range candidates {
			known[candidate.ID] = struct{}{}
		}
		missingIDs := make([]uint, 0)
		for index, id := range vectorCandidateIDs {
			vectorBoosts[id] = 0.25 * (1 - float64(index)/float64(len(vectorCandidateIDs)+1))
			if _, ok := known[id]; !ok {
				missingIDs = append(missingIDs, id)
			}
		}
		if len(missingIDs) > 0 {
			var vectorCandidates []DiscoveryCandidate
			if err := db.Where("id IN ?", missingIDs).
				Where("processing_state = ? AND eligibility_state = ?", discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible).
				Where("dedupe_state = ? AND (representative_id IS NULL OR representative_id = id)", discovery.DiscoveryDedupeReady).
				Find(&vectorCandidates).Error; err != nil {
				return nil, err
			}
			candidates = append(candidates, vectorCandidates...)
		}
	}
	seenCandidateIDs, seenDedupeKeys, err := loadPreviouslyRecommendedIdentity(userID)
	if err != nil {
		return nil, err
	}
	blockRules, err := ListUserBlockRules(userID, true)
	if err != nil {
		return nil, err
	}
	topicWeights := parseWeightMap(profile.TopicWeights)
	sourceWeights := parseWeightMap(profile.SourceWeights)
	styleWeights := parseWeightMap(profile.StyleWeights)
	personalStates := make(map[uint]discovery.UserCandidateState)
	if len(candidates) > 0 {
		candidateIDs := make([]uint, 0, len(candidates))
		for _, candidate := range candidates {
			candidateIDs = append(candidateIDs, candidate.ID)
		}
		var states []discovery.UserCandidateState
		if err := db.Where("user_id = ? AND candidate_id IN ?", userID, candidateIDs).Find(&states).Error; err != nil {
			return nil, err
		}
		for _, state := range states {
			personalStates[state.CandidateID] = state
		}
	}

	scored := make([]recommendationCandidateScore, 0, len(candidates))
	for _, candidate := range candidates {
		if state, ok := personalStates[candidate.ID]; ok && userCandidateStateExcludesRecommendation(state) {
			continue
		}
		if seenCandidateIDs[candidate.ID] {
			continue
		}
		dedupeKey := strings.TrimSpace(candidate.DedupeKey)
		if dedupeKey != "" && seenDedupeKeys[dedupeKey] {
			continue
		}
		topics := parseStringList(candidate.Topics)
		host := sourceHost(candidate.URL)
		if candidateBlocked(candidate, topics, host, blockRules) {
			continue
		}
		score := scoreRecommendationCandidate(candidate, topics, host, topicWeights, sourceWeights, styleWeights, profile.DepthPreference)
		score += vectorBoosts[candidate.ID]
		scored = append(scored, recommendationCandidateScore{
			Candidate:      candidate,
			Topics:         topics,
			SourceHost:     host,
			RetrievalScore: score,
			FinalScore:     score,
			Reason:         recommendationReason(candidate, topics, score),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].FinalScore != scored[j].FinalScore {
			return scored[i].FinalScore > scored[j].FinalScore
		}
		return scored[i].Candidate.ID < scored[j].Candidate.ID
	})
	return diversifyRecommendationCandidates(scored, selectionLimit), nil
}

func diversifyRecommendationCandidates(candidates []recommendationCandidateScore, limit int) []recommendationCandidateScore {
	if limit <= 0 || len(candidates) == 0 {
		return []recommendationCandidateScore{}
	}
	selected := make([]recommendationCandidateScore, 0, limit)
	usedDedupe := make(map[string]struct{})
	sourceCounts := make(map[string]int)
	topicCounts := make(map[string]int)
	maxSource := maxInt(1, int(float64(limit)*0.3+0.999))
	maxTopic := maxInt(1, int(float64(limit)*0.4+0.999))

	for len(selected) < limit {
		bestIndex := -1
		bestScore := -1.0
		for index, candidate := range candidates {
			if candidate.Candidate.ID == 0 {
				continue
			}
			if _, ok := usedDedupe[strings.TrimSpace(candidate.Candidate.DedupeKey)]; ok && strings.TrimSpace(candidate.Candidate.DedupeKey) != "" {
				continue
			}
			if sourceCounts[firstNonEmpty(candidate.Candidate.SourceName, candidate.SourceHost)] >= maxSource {
				continue
			}
			if dominantTopicCount(candidate.Topics, topicCounts) >= maxTopic {
				continue
			}
			diversityPenalty := maxSimilarityPenalty(candidate, selected)
			score := candidate.FinalScore - diversityPenalty
			if bestIndex == -1 || score > bestScore {
				bestIndex = index
				bestScore = score
			}
		}
		if bestIndex == -1 {
			break
		}
		chosen := candidates[bestIndex]
		chosen.FinalScore = bestScore
		selected = append(selected, chosen)
		candidates[bestIndex].Candidate.ID = 0
		if key := strings.TrimSpace(chosen.Candidate.DedupeKey); key != "" {
			usedDedupe[key] = struct{}{}
		}
		sourceCounts[firstNonEmpty(chosen.Candidate.SourceName, chosen.SourceHost)]++
		for _, topic := range chosen.Topics {
			topicCounts[topic]++
		}
	}
	if len(selected) >= limit {
		return selected
	}
	for _, candidate := range candidates {
		if len(selected) >= limit {
			break
		}
		if candidate.Candidate.ID == 0 {
			continue
		}
		if key := strings.TrimSpace(candidate.Candidate.DedupeKey); key != "" {
			if _, ok := usedDedupe[key]; ok {
				continue
			}
			usedDedupe[key] = struct{}{}
		}
		selected = append(selected, candidate)
	}
	return selected
}

func applyRecommendationReranker(ctx context.Context, userID uint, requestedCount int, candidates []recommendationCandidateScore, profile *UserRecommendationProfile, reranker RerankProvider) ([]recommendationCandidateScore, string, string, string) {
	if requestedCount <= 0 {
		requestedCount = 10
	}
	if len(candidates) == 0 {
		return []recommendationCandidateScore{}, "", "", ""
	}
	if reranker == nil {
		return trimRecommendationCandidates(candidates, requestedCount), "", "", ""
	}
	input := RerankInput{
		UserID:          userID,
		RequestedCount:  requestedCount,
		Candidates:      buildRerankCandidates(candidates),
		UserProfileHint: buildUserProfileHint(profile),
	}
	result, err := reranker.Rerank(ctx, input)
	if err != nil {
		return trimRecommendationCandidates(candidates, requestedCount), "", "", "reranker_unavailable: " + truncateError(err.Error(), 300)
	}
	byID := make(map[uint]recommendationCandidateScore, len(candidates))
	for _, candidate := range candidates {
		byID[candidate.Candidate.ID] = candidate
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		if result.Items[i].Rank != result.Items[j].Rank {
			return result.Items[i].Rank < result.Items[j].Rank
		}
		return i < j
	})
	seen := make(map[uint]struct{})
	reranked := make([]recommendationCandidateScore, 0, requestedCount)
	for _, item := range result.Items {
		candidate, ok := byID[item.CandidateID]
		if !ok {
			continue
		}
		if _, ok := seen[item.CandidateID]; ok {
			continue
		}
		if strings.TrimSpace(item.Reason) != "" {
			candidate.Reason = strings.TrimSpace(item.Reason)
		}
		candidate.RerankScore = clampScore(item.Confidence)
		candidate.RerankRank = item.Rank
		if candidate.RerankRank <= 0 {
			candidate.RerankRank = len(reranked) + 1
		}
		candidate.FinalScore += candidate.RerankScore * 0.05
		reranked = append(reranked, candidate)
		seen[item.CandidateID] = struct{}{}
		if len(reranked) >= requestedCount {
			break
		}
	}
	if len(reranked) == 0 {
		return trimRecommendationCandidates(candidates, requestedCount), "", "", "reranker_invalid_output"
	}
	for _, candidate := range candidates {
		if len(reranked) >= requestedCount {
			break
		}
		if _, ok := seen[candidate.Candidate.ID]; ok {
			continue
		}
		candidate.RerankRank = len(reranked) + 1
		reranked = append(reranked, candidate)
	}
	return reranked, strings.TrimSpace(result.Model), strings.TrimSpace(result.PromptVersion), ""
}

func buildRerankCandidates(candidates []recommendationCandidateScore) []RerankCandidate {
	items := make([]RerankCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, RerankCandidate{
			CandidateID:  candidate.Candidate.ID,
			Title:        candidate.Candidate.Title,
			Summary:      candidate.Candidate.Summary,
			Topics:       candidate.Topics,
			Source:       firstNonEmpty(candidate.Candidate.SourceName, candidate.SourceHost),
			PublishedAt:  candidate.Candidate.PublishedAt,
			QualityScore: candidate.Candidate.QualityScore,
			DepthScore:   candidate.Candidate.DepthScore,
		})
	}
	return items
}

func buildUserProfileHint(profile *UserRecommendationProfile) string {
	if profile == nil {
		return ""
	}
	return strings.Join([]string{
		"topics=" + strings.TrimSpace(profile.TopicWeights),
		"styles=" + strings.TrimSpace(profile.StyleWeights),
	}, "\n")
}

func trimRecommendationCandidates(candidates []recommendationCandidateScore, limit int) []recommendationCandidateScore {
	if limit <= 0 || len(candidates) <= limit {
		return candidates
	}
	return candidates[:limit]
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

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func isRuleBasedEnrichmentProvider(provider EnrichmentProvider) bool {
	switch provider.(type) {
	case RuleBasedEnrichmentProvider, *RuleBasedEnrichmentProvider:
		return true
	default:
		return false
	}
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

func loadPreviouslyRecommendedIdentity(userID uint) (map[uint]bool, map[string]bool, error) {
	candidateIDs := make(map[uint]bool)
	dedupeKeys := make(map[string]bool)
	var items []RecommendationItem
	if err := db.Where("user_id = ?", userID).Find(&items).Error; err != nil {
		return nil, nil, err
	}
	for _, item := range items {
		candidateIDs[item.CandidateID] = true
		if key := strings.TrimSpace(item.DedupeKey); key != "" {
			dedupeKeys[key] = true
		}
	}
	return candidateIDs, dedupeKeys, nil
}

func scoreRecommendationCandidate(candidate DiscoveryCandidate, topics []string, sourceHost string, topicWeights map[string]float64, sourceWeights map[string]float64, styleWeights map[string]float64, depthPreference float64) float64 {
	score := 0.15 + clampScore(candidate.QualityScore)*0.25 + clampScore(candidate.DepthScore)*0.15 + freshnessScore(candidate.PublishedAt)*0.15
	for _, topic := range topics {
		score += boundedWeight(topicWeights[topic]) * 0.18
	}
	for _, source := range []string{candidate.SourceName, sourceHost} {
		score += boundedWeight(sourceWeights[source]) * 0.08
	}
	for _, style := range []string{candidate.ContentStyle, candidate.ContentType} {
		score += boundedWeight(styleWeights[style]) * 0.08
	}
	if depthPreference > 0.5 {
		score += candidate.DepthScore * (depthPreference - 0.5) * 0.2
	}
	if len(topics) == 0 {
		score += 0.02
	}
	return score
}

func freshnessScore(publishedAt *time.Time) float64 {
	if publishedAt == nil {
		return 0.35
	}
	age := time.Since(*publishedAt)
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

func boundedWeight(value float64) float64 {
	if value > 2 {
		return 2
	}
	if value < -2 {
		return -2
	}
	return value
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

func parseStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func parseWeightMap(raw string) map[string]float64 {
	weights := make(map[string]float64)
	if strings.TrimSpace(raw) == "" {
		return weights
	}
	_ = json.Unmarshal([]byte(raw), &weights)
	return weights
}

func candidateBlocked(candidate DiscoveryCandidate, topics []string, host string, rules []UserBlockRule) bool {
	if len(rules) == 0 {
		return false
	}
	for _, rule := range rules {
		if !rule.Active {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(rule.RuleValue))
		if value == "" {
			continue
		}
		switch rule.RuleType {
		case UserBlockRuleTopic:
			for _, topic := range topics {
				if strings.ToLower(topic) == value {
					return true
				}
			}
		case UserBlockRuleSource:
			if strings.ToLower(candidate.SourceName) == value || strings.ToLower(host) == value {
				return true
			}
		case UserBlockRuleStyle:
			if strings.ToLower(candidate.ContentStyle) == value || strings.ToLower(candidate.ContentType) == value {
				return true
			}
		}
	}
	return false
}

func sourceHost(rawURL string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func recommendationReason(candidate DiscoveryCandidate, topics []string, score float64) string {
	if len(topics) > 0 && candidate.SourceName != "" {
		return "基于主题 " + topics[0] + " 和来源 " + candidate.SourceName + " 推荐"
	}
	if len(topics) > 0 {
		return "基于主题 " + topics[0] + " 推荐"
	}
	if candidate.QualityScore >= 0.7 {
		return "基于内容质量推荐"
	}
	if score > 0.5 {
		return "基于新鲜度和内容相关性推荐"
	}
	return "探索推荐"
}

func buildReasonMetadata(scored recommendationCandidateScore) string {
	metadata, _ := json.Marshal(map[string]interface{}{
		"topics":            scored.Topics,
		"sourceHost":        scored.SourceHost,
		"poolTags":          scored.PoolTags,
		"explorationReason": scored.ExplorationReason,
		"contentUpdated":    scored.ContentUpdated,
		"cooldownRepeat":    scored.CooldownRepeat,
	})
	return string(metadata)
}

func dominantTopicCount(topics []string, counts map[string]int) int {
	maxCount := 0
	for _, topic := range topics {
		if counts[topic] > maxCount {
			maxCount = counts[topic]
		}
	}
	return maxCount
}

func maxSimilarityPenalty(candidate recommendationCandidateScore, selected []recommendationCandidateScore) float64 {
	penalty := 0.0
	for _, item := range selected {
		if firstNonEmpty(candidate.Candidate.SourceName, candidate.SourceHost) == firstNonEmpty(item.Candidate.SourceName, item.SourceHost) {
			penalty = maxFloat(penalty, 0.08)
		}
		if sharedTopic(candidate.Topics, item.Topics) {
			penalty = maxFloat(penalty, 0.12)
		}
		if candidate.Candidate.DuplicateClusterID != "" && candidate.Candidate.DuplicateClusterID == item.Candidate.DuplicateClusterID {
			penalty = maxFloat(penalty, 0.5)
		}
	}
	return penalty
}

func sharedTopic(left []string, right []string) bool {
	seen := make(map[string]struct{})
	for _, value := range left {
		seen[strings.ToLower(value)] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[strings.ToLower(value)]; ok {
			return true
		}
	}
	return false
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func maxFloat(left float64, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func truncateError(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
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

func normalizeFeedbackAction(action string) string {
	switch strings.TrimSpace(action) {
	case RecommendationFeedbackValuable:
		return RecommendationFeedbackValuable
	case RecommendationFeedbackNotInterested:
		return RecommendationFeedbackNotInterested
	case RecommendationFeedbackDuplicate:
		return RecommendationFeedbackDuplicate
	case RecommendationFeedbackTooRepetitive:
		return RecommendationFeedbackDuplicate
	case RecommendationFeedbackDeepRead:
		return RecommendationFeedbackDeepRead
	case RecommendationFeedbackBlock:
		return RecommendationFeedbackBlock
	case RecommendationFeedbackBlockSource:
		return RecommendationFeedbackBlock
	case RecommendationFeedbackReduceTopic:
		return RecommendationFeedbackReduceTopic
	case RecommendationFeedbackReduceStyle:
		return RecommendationFeedbackReduceStyle
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
