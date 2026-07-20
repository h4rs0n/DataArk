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
	ID                  uint       `json:"id" gorm:"primaryKey"`
	SourceID            uint       `json:"sourceId" gorm:"index;not null"`
	SourceName          string     `json:"sourceName" gorm:"size:255"`
	URL                 string     `json:"url" gorm:"uniqueIndex;not null;size:2048"`
	CrawlHost           string     `json:"-" gorm:"index;not null;default:'';size:512"`
	CanonicalURL        string     `json:"canonicalUrl" gorm:"size:2048"`
	NormalizedURL       string     `json:"normalizedUrl" gorm:"index;size:2048"`
	FinalURL            string     `json:"finalUrl" gorm:"size:2048"`
	Title               string     `json:"title" gorm:"size:1024"`
	Summary             string     `json:"summary" gorm:"type:text"`
	Author              string     `json:"author" gorm:"size:255"`
	BodyText            string     `json:"bodyText" gorm:"type:text"`
	Language            string     `json:"language" gorm:"size:32"`
	WordCount           int        `json:"wordCount" gorm:"not null;default:0"`
	ContentHash         string     `json:"contentHash" gorm:"index;size:128"`
	ContentVersion      uint       `json:"contentVersion" gorm:"not null;default:0"`
	BodyChangedAt       *time.Time `json:"bodyChangedAt"`
	DedupeKey           string     `json:"dedupeKey" gorm:"index;size:128"`
	DuplicateClusterID  string     `json:"duplicateClusterId" gorm:"index;size:128"`
	RepresentativeID    *uint      `json:"representativeId" gorm:"index"`
	Topics              string     `json:"topics" gorm:"type:text"`
	Entities            string     `json:"entities" gorm:"type:text"`
	ContentType         string     `json:"contentType" gorm:"index;size:64"`
	ContentStyle        string     `json:"contentStyle" gorm:"index;size:64"`
	MetadataConfidence  int        `json:"metadataConfidence" gorm:"not null;default:0"`
	QualityScore        float64    `json:"qualityScore" gorm:"not null;default:0"`
	DepthScore          float64    `json:"depthScore" gorm:"not null;default:0"`
	EnrichmentStatus    string     `json:"enrichmentStatus" gorm:"index;size:32"`
	EnrichmentError     string     `json:"enrichmentError" gorm:"type:text"`
	EmbeddingModel      string     `json:"embeddingModel" gorm:"size:255"`
	LLMModel            string     `json:"llmModel" gorm:"size:255"`
	PromptVersion       string     `json:"promptVersion" gorm:"size:64"`
	EnrichedAt          *time.Time `json:"enrichedAt"`
	Status              string     `json:"status" gorm:"index;not null;size:32"`
	ProcessingState     string     `json:"processingState" gorm:"index;not null;default:discovered;size:32"`
	ProcessingAttempts  uint       `json:"processingAttempts" gorm:"not null;default:0"`
	ProcessingError     string     `json:"processingError" gorm:"type:text"`
	ProcessingErrorType string     `json:"processingErrorType" gorm:"index;size:64"`
	NextProcessingAt    *time.Time `json:"nextProcessingAt" gorm:"index"`
	FetchedAt           *time.Time `json:"fetchedAt"`
	ExtractedAt         *time.Time `json:"extractedAt"`
	DedupeState         string     `json:"dedupeState" gorm:"index;not null;default:pending;size:32"`
	AssessmentState     string     `json:"assessmentState" gorm:"index;not null;default:pending;size:32"`
	CurrentAssessmentID *uint      `json:"currentAssessmentId" gorm:"index"`
	AssessmentError     string     `json:"assessmentError" gorm:"type:text"`
	EligibilityState    string     `json:"eligibilityState" gorm:"index;not null;default:unknown;size:32"`
	EligibilityReasons  string     `json:"eligibilityReasons" gorm:"type:text"`
	Score               float64    `json:"score" gorm:"not null;default:0"`
	PublishedAt         *time.Time `json:"publishedAt"`
	PublishedConfidence string     `json:"publishedConfidence" gorm:"size:32"`
	ArchivedTaskID      string     `json:"archivedTaskId" gorm:"size:36"`
	LastSeenAt          time.Time  `json:"lastSeenAt" gorm:"index"`
	FirstSeenAt         *time.Time `json:"firstSeenAt" gorm:"index"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
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
