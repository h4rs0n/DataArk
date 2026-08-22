package recommendation

import (
	"DataArk/config"
	"DataArk/discovery"
	"DataArk/observability"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// digest.go 负责生成、发布与补充每日推荐日报。

// GenerateDailyRecommendations 使用生产 reranker 生成当日日报。
func GenerateDailyRecommendations(ctx context.Context, userID uint, date string) (*RecommendationDaySnapshot, error) {
	return GenerateDailyRecommendationsWithReranker(ctx, userID, date, ConfiguredRecommendationReranker())
}

// GenerateDailyRecommendationsWithReranker 使用指定 reranker 生成当日日报。
func GenerateDailyRecommendationsWithReranker(ctx context.Context, userID uint, date string, reranker RerankProvider) (*RecommendationDaySnapshot, error) {
	return generateDailyRecommendationsWithOptions(ctx, userID, date, reranker, false)
}

// RegenerateDailyRecommendations 兼容旧入口：已发布日报保持不变。
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
		observability.Log(observability.Event{Name: "recommendation_day_failed", OccurredAt: recommendationClock.Now(), UserID: userID, DayID: day.ID, LocalDate: date, Status: RecommendationDayStatusFailed, ErrorType: "selection"})
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
		observability.Log(observability.Event{Name: "recommendation_day_failed", OccurredAt: now, UserID: userID, DayID: day.ID, LocalDate: date, Status: RecommendationDayStatusFailed, ErrorType: "publish"})
		return nil, err
	}
	observability.Log(observability.Event{Name: "recommendation_day_published", OccurredAt: now, UserID: userID, DayID: day.ID, LocalDate: date, Status: RecommendationDayStatusPublished, Count: len(items)})
	return GetRecommendationDaySnapshot(userID, day.RecommendationDate)
}

func buildRecommendationItems(dayID uint, userID uint, selected []recommendationCandidateScore, firstRank int, profileVersion uint, supplemental bool, now time.Time) []RecommendationItem {
	items := make([]RecommendationItem, 0, len(selected))
	for index, scored := range selected {
		item := RecommendationItem{
			DayID: uintPointer(dayID), UserID: userID, CandidateID: scored.Candidate.ID,
			DedupeKey: strings.TrimSpace(scored.Candidate.DedupeKey), AssessmentID: scored.Candidate.CurrentAssessmentID,
			Rank: firstRank + index, RetrievalScore: scored.RetrievalScore, RerankScore: scored.RerankScore,
			FinalScore: scored.FinalScore, Reason: scored.Reason, ReasonMetadata: buildReasonMetadata(scored),
			SnapshotTitle: scored.Candidate.Title, SnapshotURL: scored.Candidate.URL,
			SnapshotSummary: scored.Candidate.Summary, SnapshotAuthor: scored.Candidate.Author,
			SnapshotSource: scored.Candidate.SourceName, SnapshotPublishedAt: scored.Candidate.PublishedAt,
			SnapshotTopics: scored.Candidate.Topics, SnapshotContentType: scored.Candidate.ContentType,
			SnapshotStyle: scored.Candidate.ContentStyle, SnapshotLanguage: scored.Candidate.Language,
			SnapshotWordCount:       scored.Candidate.WordCount,
			SnapshotProcessingState: scored.Candidate.ProcessingState, SnapshotEligibilityState: scored.Candidate.EligibilityState,
			SnapshotDedupeState: scored.Candidate.DedupeState, SnapshotClusterID: scored.Candidate.DuplicateClusterID,
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

// SupplementDailyRecommendations 为已发布但仍缺篇的日报补文。
func SupplementDailyRecommendations(ctx context.Context, userID uint, date string) (*RecommendationDaySnapshot, error) {
	return SupplementDailyRecommendationsWithReranker(ctx, userID, date, ConfiguredRecommendationReranker())
}

// SupplementDailyRecommendationsWithReranker 使用指定 reranker 为日报补文。
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
	selected, relaxations := diversifyRecommendationCandidatesV3WithReserved(reranked, missing, settings.ExplorationRate, recommendationSourceCountsFromItems(snapshot.Items))
	now := recommendationClock.Now()
	items := buildRecommendationItems(day.ID, userID, selected, day.ActualCount+1, profile.ProfileVersion, true, now)
	if err := appendRecommendationSupplement(day.ID, userID, day.RequestedCount, items, selection, relaxations, rerankModel, rerankPrompt, degradationReason, now); err != nil {
		return nil, err
	}
	if len(items) > 0 {
		observability.Log(observability.Event{Name: "recommendation_day_supplemented", OccurredAt: now, UserID: userID, DayID: day.ID, LocalDate: day.RecommendationDate, Status: RecommendationDayStatusSupplemented, Count: len(items)})
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
