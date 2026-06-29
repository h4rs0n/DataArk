package discovery

import (
	"time"

	"gorm.io/gorm"
)

var db *gorm.DB

type DiscoverySource struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	Name          string     `json:"name" gorm:"not null;size:255"`
	URL           string     `json:"url" gorm:"uniqueIndex;not null;size:2048"`
	Type          string     `json:"type" gorm:"not null;size:32"`
	Enabled       bool       `json:"enabled" gorm:"not null;default:true"`
	ETag          string     `json:"etag" gorm:"size:1024"`
	LastModified  string     `json:"lastModified" gorm:"size:1024"`
	FailureCount  int        `json:"failureCount" gorm:"not null;default:0"`
	NextFetchAt   *time.Time `json:"nextFetchAt"`
	CrawlConfig   string     `json:"crawlConfig" gorm:"type:text"`
	LastFetchedAt *time.Time `json:"lastFetchedAt"`
	LastError     string     `json:"lastError" gorm:"type:text"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type DiscoveryCandidate struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	SourceID           uint       `json:"sourceId" gorm:"index;not null"`
	SourceName         string     `json:"sourceName" gorm:"size:255"`
	URL                string     `json:"url" gorm:"uniqueIndex;not null;size:2048"`
	CanonicalURL       string     `json:"canonicalUrl" gorm:"size:2048"`
	NormalizedURL      string     `json:"normalizedUrl" gorm:"index;size:2048"`
	Title              string     `json:"title" gorm:"size:1024"`
	Summary            string     `json:"summary" gorm:"type:text"`
	Author             string     `json:"author" gorm:"size:255"`
	BodyText           string     `json:"bodyText" gorm:"type:text"`
	Language           string     `json:"language" gorm:"size:32"`
	WordCount          int        `json:"wordCount" gorm:"not null;default:0"`
	ContentHash        string     `json:"contentHash" gorm:"index;size:128"`
	DedupeKey          string     `json:"dedupeKey" gorm:"index;size:128"`
	DuplicateClusterID string     `json:"duplicateClusterId" gorm:"index;size:128"`
	Topics             string     `json:"topics" gorm:"type:text"`
	Entities           string     `json:"entities" gorm:"type:text"`
	ContentType        string     `json:"contentType" gorm:"index;size:64"`
	ContentStyle       string     `json:"contentStyle" gorm:"index;size:64"`
	QualityScore       float64    `json:"qualityScore" gorm:"not null;default:0"`
	DepthScore         float64    `json:"depthScore" gorm:"not null;default:0"`
	EnrichmentStatus   string     `json:"enrichmentStatus" gorm:"index;size:32"`
	EnrichmentError    string     `json:"enrichmentError" gorm:"type:text"`
	EmbeddingModel     string     `json:"embeddingModel" gorm:"size:255"`
	LLMModel           string     `json:"llmModel" gorm:"size:255"`
	PromptVersion      string     `json:"promptVersion" gorm:"size:64"`
	EnrichedAt         *time.Time `json:"enrichedAt"`
	Status             string     `json:"status" gorm:"index;not null;size:32"`
	Score              float64    `json:"score" gorm:"not null;default:0"`
	PublishedAt        *time.Time `json:"publishedAt"`
	ArchivedTaskID     string     `json:"archivedTaskId" gorm:"size:36"`
	LastSeenAt         time.Time  `json:"lastSeenAt" gorm:"index"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type DiscoveryCandidateFeedback struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	CandidateID uint      `json:"candidateId" gorm:"index;not null"`
	Action      string    `json:"action" gorm:"index;not null;size:32"`
	CreatedAt   time.Time `json:"createdAt" gorm:"index"`
}

func SetDB(database *gorm.DB) *gorm.DB {
	oldDB := db
	db = database
	return oldDB
}
