package dedupe

import "time"

type Fingerprint struct {
	RepresentationID uint      `gorm:"primaryKey;autoIncrement:false" json:"representationId"`
	Algorithm        string    `gorm:"primaryKey;size:64" json:"algorithm"`
	NumbersHash      string    `json:"-"`
	Signature        []byte    `json:"-"`
	Characters       int       `json:"characters"`
	CreatedAt        time.Time `json:"createdAt"`
}

func (Fingerprint) TableName() string { return "article_dedupe_fingerprints" }

type Bucket struct {
	Algorithm        string `gorm:"primaryKey;size:64;index:idx_article_dedupe_bucket,priority:1"`
	Band             int    `gorm:"primaryKey;index:idx_article_dedupe_bucket,priority:2"`
	Hash             string `gorm:"primaryKey;size:32;index:idx_article_dedupe_bucket,priority:3"`
	RepresentationID uint   `gorm:"primaryKey;autoIncrement:false"`
}

func (Bucket) TableName() string { return "article_dedupe_buckets" }

type Run struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	Algorithm     string `json:"algorithm"`
	Status        string `gorm:"index" json:"status"`
	HighWaterID   uint   `json:"highWaterId"`
	Cursor        uint   `json:"cursor"`
	PreviousRunID uint   `json:"previousRunId"`
	// AutoApply skips the operator preview checkpoint: previewing flows straight
	// into applying. Only the assessment orchestration sets it; manual runs stop
	// at ready so an owner can inspect the preview first.
	AutoApply bool      `json:"autoApply"`
	Indexed   int64     `json:"indexed"`
	Processed int64     `json:"processed"`
	Applied   int64     `json:"applied"`
	Cancelled int64     `json:"cancelled"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Run) TableName() string { return "article_dedupe_runs" }

type Member struct {
	RunID            uint      `gorm:"primaryKey;autoIncrement:false;index:idx_article_dedupe_members_rep,priority:1" json:"runId"`
	RepresentationID uint      `gorm:"primaryKey;autoIncrement:false" json:"representationId"`
	MaterialID       uint      `gorm:"index" json:"materialId"`
	VersionID        uint      `json:"versionId"`
	CandidateID      uint      `json:"candidateId"`
	RepresentativeID uint      `gorm:"index:idx_article_dedupe_members_rep,priority:2" json:"representativeRepresentationId"`
	Similarity       float64   `json:"similarity"`
	Method           string    `json:"method"`
	Revision         uint      `json:"revision"`
	BaselinePending  bool      `json:"baselinePending"`
	CandidateCount   int       `json:"candidateCount"`
	DurationMS       int64     `json:"durationMs"`
	Applied          bool      `json:"applied"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func (Member) TableName() string { return "article_dedupe_members" }

// Changes retain candidate projection snapshots, including independent eligibility.
// Rollback never alters immutable content or assessment rows.
type Change struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	RunID       uint   `gorm:"uniqueIndex:idx_article_dedupe_change,priority:1" json:"runId"`
	CandidateID uint   `gorm:"uniqueIndex:idx_article_dedupe_change,priority:2" json:"candidateId"`
	VersionID   uint   `json:"versionId"`
	Revision    uint   `json:"revision"`
	Before      string `json:"-"`
	After       string `json:"-"`
}

func (Change) TableName() string { return "article_dedupe_changes" }

type Setting struct {
	ID          uint `gorm:"primaryKey;autoIncrement:false"`
	ActiveRunID uint
	Mode        string
	// PendingAssessment records that an owner requested an assessment run while
	// suppression was not live yet. It is cleared only after the deferred run is
	// released, so a crash between activation and release is repaired by recovery.
	PendingAssessment bool
}

func (Setting) TableName() string { return "article_dedupe_settings" }
func Models() []interface{} {
	return []interface{}{&Fingerprint{}, &Bucket{}, &Run{}, &Member{}, &Change{}, &Setting{}}
}
