package material

import (
	"gorm.io/gorm"
	"time"
)

// CandidateReference resolves the old HTTP identifier without confusing it with
// a material identifier. Candidate IDs remain crawl identities permanently.
func CandidateReference(tx *gorm.DB, candidateID uint) (uint, *uint, error) {
	var row struct {
		MaterialID       uint
		CurrentVersionID *uint
	}
	err := tx.Table("discovery_candidates c").Select("c.material_id, m.current_version_id").
		Joins("JOIN material m ON m.id = c.material_id").Where("c.id = ?", candidateID).Take(&row).Error
	return row.MaterialID, row.CurrentVersionID, err
}

type Embedding struct {
	RepresentationID uint   `gorm:"primaryKey;autoIncrement:false"`
	Model            string `gorm:"primaryKey"`
	UpdatedAt        time.Time
}

func (Embedding) TableName() string { return "material_embeddings" }
