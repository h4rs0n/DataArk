package recommendation

import (
	"DataArk/discovery"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// feedback.go 负责推荐反馈、屏蔽规则与候选互动状态同步。

// RecordRecommendationFeedback 记录用户对推荐条目的反馈并同步屏蔽规则。
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
			rule.CreatedAt = now
			rule.UpdatedAt = now
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

// RevertRecommendationFeedback 撤销当前反馈并关闭关联屏蔽规则。
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
		return tx.Model(&discovery.UserCandidateState{}).Where("user_id = ? AND material_id = (SELECT material_id FROM discovery_candidates WHERE id = ?)", userID, item.CandidateID).Updates(map[string]interface{}{
			"current_feedback": "", "feedback_revoked": now, "updated_at": now,
		}).Error
	})
}

// GetCurrentRecommendationFeedback 读取条目的当前反馈。
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

// ListRecommendationFeedbackHistory 按时间列出条目的反馈历史。
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
		Columns: []clause.Column{{Name: "user_id"}, {Name: "material_id"}}, DoNothing: true,
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
	case RecommendationFeedbackValuable, RecommendationFeedbackNotInterested, RecommendationFeedbackDuplicate, RecommendationFeedbackLowValue,
		RecommendationFeedbackBlock, RecommendationFeedbackReduceTopic, RecommendationFeedbackReduceStyle:
	default:
		return nil
	}
	return tx.Model(&discovery.UserCandidateState{}).Where("user_id = ? AND material_id = (SELECT material_id FROM discovery_candidates WHERE id = ?)", userID, candidateID).Updates(updates).Error
}

// ListUserBlockRules 列出用户屏蔽规则。
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

// DeleteUserBlockRule 停用一条用户屏蔽规则。
func DeleteUserBlockRule(userID uint, ruleID uint) error {
	if db == nil || userID == 0 || ruleID == 0 {
		return nil
	}
	return db.Model(&UserBlockRule{}).Where("id = ? AND user_id = ?", ruleID, userID).Updates(map[string]interface{}{
		"active":     false,
		"updated_at": time.Now(),
	}).Error
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
	case RecommendationFeedbackLowValue:
		return RecommendationFeedbackLowValue
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
