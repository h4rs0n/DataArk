package discovery

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	UserCandidateFeedbackNotInterested = "not_interested"
	UserCandidateFeedbackValuable      = "valuable"
	UserCandidateFeedbackDuplicate     = "duplicate"
	UserCandidateFeedbackDeepRead      = "deep_read"
)

type UserDiscoveryCandidate struct {
	DiscoveryCandidate
	UserState *UserCandidateState `json:"userState,omitempty"`
}

func ListDiscoveryCandidatesForUser(userID uint, status string, limit int) ([]UserDiscoveryCandidate, error) {
	views := make([]UserDiscoveryCandidate, 0)
	if db == nil || userID == 0 {
		return views, nil
	}
	limit = normalizeLimit(limit, 50, 200)
	var candidates []DiscoveryCandidate
	queryLimit := limit
	personalStatus := isPersonalCandidateStatus(status)
	if personalStatus {
		queryLimit = 200
	}
	query := db.Order("score desc, published_at desc, last_seen_at desc").Limit(queryLimit)
	if strings.TrimSpace(status) != "" && !personalStatus {
		query = query.Where("status = ?", strings.TrimSpace(status))
	}
	if err := query.Find(&candidates).Error; err != nil {
		return views, err
	}
	ids := make([]uint, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	states := make(map[uint]UserCandidateState)
	if len(ids) > 0 {
		var userStates []UserCandidateState
		if err := db.Where("user_id = ? AND candidate_id IN ?", userID, ids).Find(&userStates).Error; err != nil {
			return views, err
		}
		for _, state := range userStates {
			states[state.CandidateID] = state
		}
	}
	for _, candidate := range candidates {
		view := UserDiscoveryCandidate{DiscoveryCandidate: candidate}
		if state, ok := states[candidate.ID]; ok {
			copy := state
			view.UserState = &copy
		}
		if personalStatus && !matchesPersonalCandidateStatus(status, view.UserState) {
			continue
		}
		views = append(views, view)
		if len(views) >= limit {
			break
		}
	}
	return views, nil
}

func MarkUserCandidateRead(userID uint, candidateID uint) (*UserDiscoveryCandidate, error) {
	now := discoveryClock.Now()
	state, candidate, err := updateUserCandidateState(userID, candidateID, map[string]interface{}{
		"opened_at": now, "read_at": now,
	})
	if err != nil {
		return nil, err
	}
	return &UserDiscoveryCandidate{DiscoveryCandidate: candidate, UserState: &state}, nil
}

func MarkUserCandidateIgnored(userID uint, candidateID uint) (*UserDiscoveryCandidate, error) {
	now := discoveryClock.Now()
	state, candidate, err := updateUserCandidateState(userID, candidateID, map[string]interface{}{
		"current_feedback": UserCandidateFeedbackNotInterested, "feedback_set_at": now, "feedback_revoked": nil,
	})
	if err != nil {
		return nil, err
	}
	return &UserDiscoveryCandidate{DiscoveryCandidate: candidate, UserState: &state}, nil
}

func MarkUserCandidateArchived(userID uint, candidateID uint, taskID string) (*UserDiscoveryCandidate, error) {
	now := discoveryClock.Now()
	state, candidate, err := updateUserCandidateState(userID, candidateID, map[string]interface{}{"archived_at": now})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(taskID) != "" {
		if err := db.Model(&DiscoveryCandidate{}).Where("id = ?", candidateID).Updates(map[string]interface{}{"archived_task_id": strings.TrimSpace(taskID), "updated_at": now}).Error; err != nil {
			return nil, err
		}
		candidate.ArchivedTaskID = strings.TrimSpace(taskID)
	}
	return &UserDiscoveryCandidate{DiscoveryCandidate: candidate, UserState: &state}, nil
}

func GetUserCandidateState(userID uint, candidateID uint) (*UserCandidateState, error) {
	if db == nil || userID == 0 || candidateID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var state UserCandidateState
	if err := db.Where("user_id = ? AND candidate_id = ?", userID, candidateID).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func updateUserCandidateState(userID uint, candidateID uint, updates map[string]interface{}) (UserCandidateState, DiscoveryCandidate, error) {
	var state UserCandidateState
	var candidate DiscoveryCandidate
	if db == nil || userID == 0 || candidateID == 0 {
		return state, candidate, gorm.ErrRecordNotFound
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&candidate, candidateID).Error; err != nil {
			return err
		}
		now := discoveryClock.Now()
		state = UserCandidateState{UserID: userID, CandidateID: candidateID, CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "candidate_id"}}, DoNothing: true,
		}).Create(&state).Error; err != nil {
			return err
		}
		updates["updated_at"] = now
		if err := tx.Model(&UserCandidateState{}).Where("user_id = ? AND candidate_id = ?", userID, candidateID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? AND candidate_id = ?", userID, candidateID).First(&state).Error
	})
	return state, candidate, err
}

func isPersonalCandidateStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case DiscoveryCandidateStatusNew, DiscoveryCandidateStatusRead, DiscoveryCandidateStatusIgnored, DiscoveryCandidateStatusArchived:
		return true
	default:
		return false
	}
}

func matchesPersonalCandidateStatus(status string, state *UserCandidateState) bool {
	switch strings.TrimSpace(status) {
	case DiscoveryCandidateStatusRead:
		return state != nil && state.ReadAt != nil
	case DiscoveryCandidateStatusIgnored:
		return state != nil && state.CurrentFeedback == UserCandidateFeedbackNotInterested
	case DiscoveryCandidateStatusArchived:
		return state != nil && state.ArchivedAt != nil
	case DiscoveryCandidateStatusNew:
		return state == nil || (state.ReadAt == nil && state.ArchivedAt == nil && strings.TrimSpace(state.CurrentFeedback) == "")
	default:
		return true
	}
}

func RecordUserCandidateExposure(tx *gorm.DB, userID uint, candidateID uint, exposedAt time.Time) error {
	if tx == nil || userID == 0 || candidateID == 0 {
		return errors.New("missing user candidate exposure identity")
	}
	state := UserCandidateState{UserID: userID, CandidateID: candidateID, FirstExposedAt: &exposedAt, LastExposedAt: &exposedAt, ExposureCount: 1, CreatedAt: exposedAt, UpdatedAt: exposedAt}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "candidate_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"last_exposed_at": exposedAt, "exposure_count": gorm.Expr("user_candidate_states.exposure_count + 1"), "updated_at": exposedAt,
		}),
	}).Create(&state).Error
}
