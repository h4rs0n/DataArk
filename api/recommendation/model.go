package recommendation

import (
	"DataArk/discovery"
	"time"

	"gorm.io/gorm"
)

var db *gorm.DB

type DiscoveryCandidate = discovery.DiscoveryCandidate

const (
	DiscoveryCandidateStatusNew     = discovery.DiscoveryCandidateStatusNew
	DiscoveryCandidateStatusIgnored = discovery.DiscoveryCandidateStatusIgnored
)

type RecommendationSettings struct {
	ID                  uint      `json:"id" gorm:"primaryKey"`
	UserID              uint      `json:"userId" gorm:"uniqueIndex;not null"`
	DailyLimit          int       `json:"dailyLimit" gorm:"not null;default:10"`
	Timezone            string    `json:"timezone" gorm:"not null;size:64"`
	GenerationTime      string    `json:"generationTime" gorm:"not null;size:16"`
	CandidateWindowDays int       `json:"candidateWindowDays" gorm:"not null;default:30"`
	ExplorationRate     float64   `json:"explorationRate" gorm:"not null;default:0.15"`
	PreferredTopics     string    `json:"preferredTopics" gorm:"type:text"`
	PreferredLanguages  string    `json:"preferredLanguages" gorm:"type:text"`
	PreferredLength     string    `json:"preferredLength" gorm:"size:32"`
	PreferredDepth      float64   `json:"preferredDepth"`
	FavoriteSources     string    `json:"favoriteSources" gorm:"type:text"`
	Enabled             bool      `json:"enabled" gorm:"not null;default:true"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type RecommendationDay struct {
	ID                 uint                 `json:"id" gorm:"primaryKey"`
	UserID             uint                 `json:"userId" gorm:"uniqueIndex:idx_recommendation_days_user_date;not null"`
	RecommendationDate string               `json:"date" gorm:"uniqueIndex:idx_recommendation_days_user_date;not null;size:10"`
	Timezone           string               `json:"timezone" gorm:"not null;size:64"`
	Status             string               `json:"status" gorm:"index;not null;size:32"`
	PolicyVersion      string               `json:"policyVersion" gorm:"index;not null;default:v2;size:64"`
	ShortageReasons    string               `json:"shortageReasons" gorm:"type:text"`
	RequestedCount     int                  `json:"requestedCount" gorm:"not null;default:10"`
	ActualCount        int                  `json:"actualCount" gorm:"not null;default:0"`
	ProfileVersion     uint                 `json:"profileVersion"`
	LLMModel           string               `json:"llmModel" gorm:"size:255"`
	PromptVersion      string               `json:"promptVersion" gorm:"size:64"`
	FailureReason      string               `json:"failureReason" gorm:"type:text"`
	Degraded           bool                 `json:"degraded" gorm:"index;not null;default:false"`
	DegradationReason  string               `json:"degradationReason" gorm:"type:text"`
	SupplementPolicy   string               `json:"supplementPolicy" gorm:"size:64"`
	GeneratedAt        *time.Time           `json:"generatedAt"`
	PublishedAt        *time.Time           `json:"publishedAt" gorm:"index"`
	SupplementedAt     *time.Time           `json:"supplementedAt" gorm:"index"`
	AuditVersion       uint                 `json:"auditVersion" gorm:"not null;default:1"`
	Items              []RecommendationItem `json:"items" gorm:"foreignKey:DayID"`
	CreatedAt          time.Time            `json:"createdAt"`
	UpdatedAt          time.Time            `json:"updatedAt"`
}

type RecommendationItem struct {
	ID                       uint                         `json:"id" gorm:"primaryKey"`
	DayID                    uint                         `json:"dayId" gorm:"uniqueIndex:idx_recommendation_items_day_candidate;index;not null"`
	UserID                   uint                         `json:"userId" gorm:"index;not null"`
	CandidateID              uint                         `json:"candidateId" gorm:"uniqueIndex:idx_recommendation_items_day_candidate;index;not null"`
	Candidate                discovery.DiscoveryCandidate `json:"candidate" gorm:"-"`
	DedupeKey                string                       `json:"dedupeKey" gorm:"index;size:128"`
	Rank                     int                          `json:"rank" gorm:"not null"`
	RetrievalScore           float64                      `json:"retrievalScore"`
	RerankScore              float64                      `json:"rerankScore"`
	FinalScore               float64                      `json:"finalScore"`
	Reason                   string                       `json:"reason" gorm:"type:text"`
	ReasonMetadata           string                       `json:"reasonMetadata" gorm:"type:text"`
	SnapshotTitle            string                       `json:"snapshotTitle" gorm:"size:1024"`
	SnapshotURL              string                       `json:"snapshotUrl" gorm:"size:2048"`
	SnapshotSummary          string                       `json:"snapshotSummary" gorm:"type:text"`
	SnapshotAuthor           string                       `json:"snapshotAuthor" gorm:"size:255"`
	SnapshotSource           string                       `json:"snapshotSource" gorm:"size:255"`
	SnapshotPublishedAt      *time.Time                   `json:"snapshotPublishedAt"`
	SnapshotTopics           string                       `json:"snapshotTopics" gorm:"type:text"`
	SnapshotContentType      string                       `json:"snapshotContentType" gorm:"size:64"`
	SnapshotStyle            string                       `json:"snapshotStyle" gorm:"size:64"`
	SnapshotLanguage         string                       `json:"snapshotLanguage" gorm:"size:32"`
	SnapshotWordCount        int                          `json:"snapshotWordCount"`
	SnapshotProcessingState  string                       `json:"snapshotProcessingState" gorm:"size:32"`
	SnapshotEligibilityState string                       `json:"snapshotEligibilityState" gorm:"size:32"`
	SnapshotDedupeState      string                       `json:"snapshotDedupeState" gorm:"size:32"`
	SnapshotClusterID        string                       `json:"snapshotClusterId" gorm:"size:128"`
	PoolType                 string                       `json:"poolType" gorm:"index;size:32"`
	ExplorationReason        string                       `json:"explorationReason" gorm:"type:text"`
	AssessmentID             *uint                        `json:"assessmentId" gorm:"index"`
	ContentVersion           uint                         `json:"contentVersion" gorm:"not null;default:0"`
	ContentUpdated           bool                         `json:"contentUpdated" gorm:"index;not null;default:false"`
	CooldownRepeat           bool                         `json:"cooldownRepeat" gorm:"index;not null;default:false"`
	ProfileVersion           uint                         `json:"profileVersion"`
	ModelVersion             string                       `json:"modelVersion" gorm:"size:255"`
	Supplemental             bool                         `json:"supplemental" gorm:"index;not null;default:false"`
	SupplementedAt           *time.Time                   `json:"supplementedAt"`
	AuditVersion             uint                         `json:"auditVersion" gorm:"not null;default:1"`
	CreatedAt                time.Time                    `json:"createdAt"`
	UpdatedAt                time.Time                    `json:"updatedAt"`
}

type RecommendationFeedback struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	UserID               uint       `json:"userId" gorm:"index;not null"`
	RecommendationItemID uint       `json:"recommendationItemId" gorm:"index;not null"`
	CandidateID          uint       `json:"candidateId" gorm:"index;not null"`
	Action               string     `json:"action" gorm:"index;not null;size:32"`
	Metadata             string     `json:"metadata" gorm:"type:text"`
	IsCurrent            bool       `json:"isCurrent" gorm:"index;not null;default:true"`
	CurrentKey           *string    `json:"-" gorm:"uniqueIndex;size:128"`
	SupersedesID         *uint      `json:"supersedesId" gorm:"index"`
	ClosedReason         string     `json:"closedReason" gorm:"size:32"`
	CreatedAt            time.Time  `json:"createdAt" gorm:"index"`
	RevertedAt           *time.Time `json:"revertedAt"`
}

type UserBlockRule struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	UserID     uint      `json:"userId" gorm:"index;not null"`
	RuleType   string    `json:"type" gorm:"index;not null;size:32"`
	RuleValue  string    `json:"value" gorm:"index;not null;size:255"`
	FeedbackID *uint     `json:"feedbackId" gorm:"index"`
	Active     bool      `json:"active" gorm:"index;not null;default:true"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type UserRecommendationProfile struct {
	ID                uint       `json:"id" gorm:"primaryKey"`
	UserID            uint       `json:"userId" gorm:"uniqueIndex;not null"`
	PositiveEmbedding string     `json:"positiveEmbedding" gorm:"type:text"`
	NegativeEmbedding string     `json:"negativeEmbedding" gorm:"type:text"`
	TopicWeights      string     `json:"topicWeights" gorm:"type:text"`
	SourceWeights     string     `json:"sourceWeights" gorm:"type:text"`
	StyleWeights      string     `json:"styleWeights" gorm:"type:text"`
	DepthPreference   float64    `json:"depthPreference"`
	ExplorationRate   float64    `json:"explorationRate"`
	ProfileVersion    uint       `json:"profileVersion" gorm:"not null;default:1"`
	FeedbackResetAt   *time.Time `json:"feedbackResetAt" gorm:"index"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

func SetDB(database *gorm.DB) *gorm.DB {
	oldDB := db
	db = database
	return oldDB
}
