package discovery

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

var db *gorm.DB

type DiscoverySource struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	Name          string     `json:"name" gorm:"not null;size:255"`
	URL           string     `json:"url" gorm:"uniqueIndex;not null;size:2048"`
	CrawlHost     string     `json:"-" gorm:"index;not null;default:'';size:512"`
	Type          string     `json:"type" gorm:"not null;size:32"`
	SiteID        *uint      `json:"siteId" gorm:"index"`
	EndpointType  string     `json:"endpointType" gorm:"index;not null;default:legacy;size:32"`
	UserManaged   bool       `json:"userManaged" gorm:"index;not null;default:false"`
	Priority      int        `json:"priority" gorm:"not null;default:0"`
	Enabled       bool       `json:"enabled" gorm:"not null;default:true"`
	ETag          string     `json:"etag" gorm:"column:etag;size:1024"`
	LastModified  string     `json:"lastModified" gorm:"size:1024"`
	FailureCount  int        `json:"failureCount" gorm:"not null;default:0"`
	NextFetchAt   *time.Time `json:"nextFetchAt"`
	LastAttemptAt *time.Time `json:"lastAttemptAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
	NextDueAt     *time.Time `json:"nextDueAt" gorm:"index"`
	BackoffUntil  *time.Time `json:"backoffUntil"`
	BackoffReason string     `json:"backoffReason" gorm:"size:64"`
	CrawlConfig   string     `json:"crawlConfig" gorm:"type:text"`
	LastFetchedAt *time.Time `json:"lastFetchedAt"`
	LastError     string     `json:"lastError" gorm:"type:text"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func (source *DiscoverySource) BeforeSave(tx *gorm.DB) error {
	if source.CrawlHost == "" {
		source.CrawlHost = crawlHostForURL(source.URL)
	}
	if tx != nil && tx.Dialector.Name() == "postgres" && strings.TrimSpace(source.CrawlConfig) == "" {
		source.CrawlConfig = "{}"
	}
	return nil
}

type DiscoveryCandidate struct {
	SkipInitialProvenance  bool       `json:"-" gorm:"-"`
	MaterialID             uint       `json:"materialId" gorm:"index"`
	IndependentSourceCount int        `json:"independentSourceCount" gorm:"->;-:migration"`
	ID                     uint       `json:"id" gorm:"primaryKey"`
	SourceID               uint       `json:"sourceId" gorm:"->;-:migration"`
	SourceName             string     `json:"sourceName" gorm:"->;-:migration"`
	URL                    string     `json:"url" gorm:"uniqueIndex;not null;size:2048"`
	CrawlHost              string     `json:"-" gorm:"index;not null;default:'';size:512"`
	CanonicalURL           string     `json:"canonicalUrl" gorm:"size:2048"`
	NormalizedURL          string     `json:"normalizedUrl" gorm:"index;size:2048"`
	FinalURL               string     `json:"finalUrl" gorm:"size:2048"`
	Title                  string     `json:"title" gorm:"->;-:migration"`
	Summary                string     `json:"summary" gorm:"->;-:migration"`
	Author                 string     `json:"author" gorm:"->;-:migration"`
	BodyText               string     `json:"bodyText" gorm:"->;-:migration"`
	Language               string     `json:"language" gorm:"->;-:migration"`
	WordCount              int        `json:"wordCount" gorm:"->;-:migration"`
	ContentHash            string     `json:"contentHash" gorm:"->;-:migration"`
	ContentVersion         uint       `json:"contentVersion" gorm:"->;-:migration"`
	BodyChangedAt          *time.Time `json:"bodyChangedAt" gorm:"->;-:migration"`
	DedupeKey              string     `json:"dedupeKey" gorm:"index;size:128"`
	DuplicateClusterID     string     `json:"duplicateClusterId" gorm:"index;size:128"`
	RepresentativeID       *uint      `json:"representativeId" gorm:"index"`
	Topics                 string     `json:"topics" gorm:"->;-:migration"`
	Entities               string     `json:"entities" gorm:"->;-:migration"`
	ContentType            string     `json:"contentType" gorm:"->;-:migration"`
	ContentStyle           string     `json:"contentStyle" gorm:"->;-:migration"`
	MetadataConfidence     int        `json:"metadataConfidence" gorm:"->;-:migration"`
	QualityScore           float64    `json:"qualityScore" gorm:"->;-:migration"`
	DepthScore             float64    `json:"depthScore" gorm:"->;-:migration"`
	EnrichmentStatus       string     `json:"enrichmentStatus" gorm:"->;-:migration"`
	EnrichmentError        string     `json:"enrichmentError" gorm:"->;-:migration"`
	EmbeddingModel         string     `json:"embeddingModel" gorm:"->;-:migration"`
	LLMModel               string     `json:"llmModel" gorm:"->;-:migration"`
	PromptVersion          string     `json:"promptVersion" gorm:"->;-:migration"`
	EnrichedAt             *time.Time `json:"enrichedAt" gorm:"->;-:migration"`
	Status                 string     `json:"status" gorm:"index;not null;size:32"`
	ProcessingState        string     `json:"processingState" gorm:"index;not null;default:discovered;size:32"`
	ProcessingAttempts     uint       `json:"processingAttempts" gorm:"not null;default:0"`
	ProcessingError        string     `json:"processingError" gorm:"type:text"`
	ProcessingErrorType    string     `json:"processingErrorType" gorm:"index;size:64"`
	NextProcessingAt       *time.Time `json:"nextProcessingAt" gorm:"index"`
	FetchedAt              *time.Time `json:"fetchedAt"`
	ExtractedAt            *time.Time `json:"extractedAt"`
	DedupeState            string     `json:"dedupeState" gorm:"index;not null;default:pending;size:32"`
	AssessmentState        string     `json:"assessmentState" gorm:"->;-:migration"`
	CurrentAssessmentID    *uint      `json:"currentAssessmentId" gorm:"->;-:migration"`
	AssessmentError        string     `json:"assessmentError" gorm:"->;-:migration"`
	EligibilityState       string     `json:"eligibilityState" gorm:"->;-:migration"`
	EligibilityReasons     string     `json:"eligibilityReasons" gorm:"->;-:migration"`
	Score                  float64    `json:"score" gorm:"->;-:migration"`
	PublishedAt            *time.Time `json:"publishedAt" gorm:"->;-:migration"`
	PublishedConfidence    string     `json:"publishedConfidence" gorm:"->;-:migration"`
	ArchivedTaskID         string     `json:"archivedTaskId" gorm:"->;-:migration"`
	LastSeenAt             time.Time  `json:"lastSeenAt" gorm:"index"`
	FirstSeenAt            *time.Time `json:"firstSeenAt" gorm:"index"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

func (candidate *DiscoveryCandidate) BeforeSave(tx *gorm.DB) error {
	if candidate.CrawlHost == "" {
		candidate.CrawlHost = crawlHostForURL(candidate.URL)
	}
	if tx == nil || tx.Dialector.Name() != "postgres" {
		return nil
	}
	if strings.TrimSpace(candidate.Topics) == "" {
		candidate.Topics = "[]"
	}
	if strings.TrimSpace(candidate.Entities) == "" {
		candidate.Entities = "[]"
	}
	return nil
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
