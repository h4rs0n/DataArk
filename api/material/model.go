// Package material owns content identity independently of discovery and storage.
package material

import "time"

// Material describes a work, not its URL, file, extraction or assessment job.
// Lists are JSON text in Go and JSONB in PostgreSQL.
type Material struct {
	ID               uint       `json:"id" gorm:"primaryKey"`
	Title            string     `json:"title"`
	Summary          string     `json:"summary"`
	Authors          string     `json:"authors" gorm:"not null;default:'[]'"`
	Language         string     `json:"language"`
	PublishedAt      *time.Time `json:"publishedAt"`
	Topics           string     `json:"topics" gorm:"not null;default:'[]'"`
	Entities         string     `json:"entities" gorm:"not null;default:'[]'"`
	CurrentVersionID *uint      `json:"currentVersionId"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

func (Material) TableName() string { return "material" }

type Version struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	MaterialID  uint       `json:"materialId" gorm:"uniqueIndex:idx_material_version;not null"`
	Version     uint       `json:"version" gorm:"uniqueIndex:idx_material_version;not null"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Authors     string     `json:"authors" gorm:"not null;default:'[]'"`
	Language    string     `json:"language"`
	PublishedAt *time.Time `json:"publishedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func (Version) TableName() string { return "material_versions" }

// Representation stores extracted content. Physical assets remain in archive.
// Multiple roles/modalities may coexist in a single logical content version.
type Representation struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	VersionID     uint      `json:"versionId" gorm:"uniqueIndex:idx_material_representation;not null"`
	Kind          string    `json:"kind" gorm:"uniqueIndex:idx_material_representation;not null"`
	Role          string    `json:"role" gorm:"uniqueIndex:idx_material_representation;not null"`
	Text          string    `json:"text"`
	ContentHash   string    `json:"contentHash" gorm:"index"`
	HashAlgorithm string    `json:"hashAlgorithm" gorm:"not null;default:sha256-text-v1"`
	WordCount     int       `json:"wordCount"`
	Extractor     string    `json:"extractor"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (Representation) TableName() string { return "material_representations" }

type Identity struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	MaterialID  uint      `json:"materialId" gorm:"index;not null"`
	Kind        string    `json:"kind" gorm:"uniqueIndex:idx_material_identity;not null"`
	IdentityKey string    `json:"identityKey" gorm:"uniqueIndex:idx_material_identity;not null"`
	Value       string    `json:"value"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (Identity) TableName() string { return "material_identities" }

type Redirect struct {
	ID         uint      `json:"id" gorm:"primaryKey;autoIncrement:false"`
	MaterialID uint      `json:"materialId" gorm:"index;not null"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (Redirect) TableName() string { return "material_redirects" }

// Provenance is an observed discovery path, not an ownership relationship.
// DomainKey is a snapshot of the discovering site, never the target URL host.
type Provenance struct {
	ID                  uint       `json:"id" gorm:"primaryKey"`
	ProvenanceKey       string     `json:"-" gorm:"uniqueIndex;not null"`
	MaterialID          uint       `json:"materialId" gorm:"index;not null"`
	CandidateID         *uint      `json:"candidateId,omitempty" gorm:"index"`
	SiteID              *uint      `json:"siteId,omitempty" gorm:"index"`
	SourceID            *uint      `json:"sourceId,omitempty" gorm:"index"`
	DomainKey           string     `json:"domainKey" gorm:"index"`
	SourceName          string     `json:"sourceName"`
	DiscoveryMethod     string     `json:"discoveryMethod"`
	OriginalURL         string     `json:"originalUrl"`
	SourcePageURL       string     `json:"sourcePageUrl"`
	Title               string     `json:"title"`
	Summary             string     `json:"summary"`
	PublishedAt         *time.Time `json:"publishedAt"`
	MetadataConfidence  int        `json:"metadataConfidence"`
	PublishedConfidence string     `json:"publishedConfidence"`
	MigrationUncertain  bool       `json:"migrationUncertain"`
	FirstSeenAt         time.Time  `json:"firstSeenAt"`
	LastSeenAt          time.Time  `json:"lastSeenAt"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

func (Provenance) TableName() string { return "material_provenances" }

// ArticleState is the current article assessment projection. It is deliberately
// outside Material: future modalities need not implement the article pipeline.
type ArticleState struct {
	MaterialID          uint       `json:"materialId" gorm:"primaryKey;autoIncrement:false"`
	ContentVersion      uint       `json:"contentVersion"`
	BodyChangedAt       *time.Time `json:"bodyChangedAt"`
	ContentType         string     `json:"contentType"`
	ContentStyle        string     `json:"contentStyle"`
	QualityScore        float64    `json:"qualityScore"`
	DepthScore          float64    `json:"depthScore"`
	AssessmentState     string     `json:"assessmentState" gorm:"index;not null;default:pending"`
	CurrentAssessmentID *uint      `json:"currentAssessmentId"`
	AssessmentError     string     `json:"assessmentError"`
	EligibilityState    string     `json:"eligibilityState" gorm:"index;not null;default:unknown"`
	EligibilityReasons  string     `json:"eligibilityReasons"`
	EnrichmentStatus    string     `json:"enrichmentStatus"`
	EnrichmentError     string     `json:"enrichmentError"`
	LLMModel            string     `json:"llmModel"`
	PromptVersion       string     `json:"promptVersion"`
	EnrichedAt          *time.Time `json:"enrichedAt"`
	Score               float64    `json:"score"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

func (ArticleState) TableName() string { return "material_article_states" }

type CandidateVersion struct {
	CandidateID    uint `gorm:"primaryKey;autoIncrement:false"`
	ContentVersion uint `gorm:"primaryKey;autoIncrement:false"`
	MaterialID     uint `gorm:"index;not null"`
	VersionID      uint `gorm:"index;not null"`
}

func (CandidateVersion) TableName() string { return "material_candidate_versions" }

type ArchiveLink struct {
	TaskID     string `gorm:"primaryKey"`
	MaterialID uint   `gorm:"index;not null"`
}

func (ArchiveLink) TableName() string { return "material_archive_links" }

func Models() []interface{} {
	return []interface{}{&Material{}, &Version{}, &Representation{}, &Identity{}, &Redirect{}, &Provenance{}, &ArticleState{}, &CandidateVersion{}, &ArchiveLink{}, &Embedding{}, &UserState{}}
}
