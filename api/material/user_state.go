package material

import (
	"gorm.io/gorm"
	"time"
)

type UserState struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	UserID          uint       `json:"userId" gorm:"uniqueIndex:idx_user_material_state;not null"`
	MaterialID      uint       `json:"materialId" gorm:"uniqueIndex:idx_user_material_state;not null"`
	CandidateID     uint       `json:"candidateId,omitempty" gorm:"index;default:null"` // Historical entry point, never the state key.
	FirstExposedAt  *time.Time `json:"firstExposedAt"`
	LastExposedAt   *time.Time `json:"lastExposedAt"`
	ExposureCount   uint       `json:"exposureCount"`
	OpenedAt        *time.Time `json:"openedAt"`
	ReadAt          *time.Time `json:"readAt"`
	DeepReadAt      *time.Time `json:"deepReadAt"`
	ArchivedAt      *time.Time `json:"archivedAt"`
	CurrentFeedback string     `json:"currentFeedback"`
	FeedbackSetAt   *time.Time `json:"feedbackSetAt"`
	FeedbackRevoked *time.Time `json:"feedbackRevoked"`
	MigratedFrom    string     `json:"migratedFrom"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

func (UserState) TableName() string { return "user_material_states" }
func (row *UserState) BeforeCreate(tx *gorm.DB) error {
	if row.MaterialID != 0 {
		return nil
	}
	id, _, err := CandidateReference(tx, row.CandidateID)
	row.MaterialID = id
	return err
}

func latest(a, b *time.Time) *time.Time {
	if a == nil || (b != nil && b.After(*a)) {
		return b
	}
	return a
}

func mergeUserStates(tx *gorm.DB, target, source uint) error {
	var states []UserState
	if err := tx.Where("material_id = ?", source).Order("id").Find(&states).Error; err != nil {
		return err
	}
	for _, incoming := range states {
		var current UserState
		result := tx.Where("material_id = ? AND user_id = ?", target, incoming.UserID).Limit(1).Find(&current)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := tx.Model(&UserState{}).Where("id = ?", incoming.ID).Update("material_id", target).Error; err != nil {
				return err
			}
			continue
		}
		current.ExposureCount += incoming.ExposureCount
		if current.FirstExposedAt == nil || (incoming.FirstExposedAt != nil && incoming.FirstExposedAt.Before(*current.FirstExposedAt)) {
			current.FirstExposedAt = incoming.FirstExposedAt
		}
		current.LastExposedAt = latest(current.LastExposedAt, incoming.LastExposedAt)
		current.OpenedAt = latest(current.OpenedAt, incoming.OpenedAt)
		current.ReadAt = latest(current.ReadAt, incoming.ReadAt)
		current.DeepReadAt = latest(current.DeepReadAt, incoming.DeepReadAt)
		current.ArchivedAt = latest(current.ArchivedAt, incoming.ArchivedAt)
		oldEvent, newEvent := latest(current.FeedbackSetAt, current.FeedbackRevoked), latest(incoming.FeedbackSetAt, incoming.FeedbackRevoked)
		if newEvent != nil && (oldEvent == nil || newEvent.After(*oldEvent) || (newEvent.Equal(*oldEvent) && incoming.ID > current.ID)) {
			current.CurrentFeedback, current.FeedbackSetAt, current.FeedbackRevoked = incoming.CurrentFeedback, incoming.FeedbackSetAt, incoming.FeedbackRevoked
		}
		if err := tx.Save(&current).Error; err != nil {
			return err
		}
		if err := tx.Delete(&UserState{}, incoming.ID).Error; err != nil {
			return err
		}
	}
	return nil
}
