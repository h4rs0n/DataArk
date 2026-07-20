package discovery

import "time"

const (
	DiscoverySiteStatusSeed      = "seed"
	DiscoverySiteStatusObserving = "observing"
	DiscoverySiteStatusActive    = "active"
	DiscoverySiteStatusPaused    = "paused"
	DiscoverySiteStatusBlocked   = "blocked"
	DiscoverySiteStatusNonBlog   = "non_blog"

	DiscoveryProcessingDiscovered    = "discovered"
	DiscoveryProcessingFetching      = "fetching"
	DiscoveryProcessingReady         = "ready"
	DiscoveryProcessingReview        = "review"
	DiscoveryProcessingFailed        = "failed"
	DiscoveryProcessingIneligible    = "ineligible"
	DiscoveryProcessingDomainBlocked = "domain_blocked"
	DiscoveryEligibilityUnknown      = "unknown"
	DiscoveryEligibilityEligible     = "eligible"
	DiscoveryEligibilityReview       = "review"
	DiscoveryEligibilityIneligible   = "ineligible"
)

type DiscoverySite struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	RootURL            string     `json:"rootUrl" gorm:"not null;size:2048"`
	HostKey            string     `json:"hostKey" gorm:"uniqueIndex;not null;size:512"`
	DomainKey          string     `json:"domainKey" gorm:"index;not null;default:'';size:512"`
	DisplayName        string     `json:"displayName" gorm:"size:255"`
	Status             string     `json:"status" gorm:"index;not null;default:observing;size:32"`
	DiscoveryMethod    string     `json:"discoveryMethod" gorm:"index;not null;default:unknown;size:64"`
	GraphDepth         int        `json:"graphDepth" gorm:"index;not null;default:0"`
	CrawlAllowed       bool       `json:"crawlAllowed" gorm:"index;not null;default:true"`
	RobotsStatus       string     `json:"robotsStatus" gorm:"index;not null;default:unknown;size:32"`
	FirstDiscoveredAt  time.Time  `json:"firstDiscoveredAt" gorm:"index;not null"`
	LastReferencedAt   *time.Time `json:"lastReferencedAt" gorm:"index"`
	LastArticleAt      *time.Time `json:"lastArticleAt"`
	LastValidatedAt    *time.Time `json:"lastValidatedAt"`
	ActivatedAt        *time.Time `json:"activatedAt" gorm:"index"`
	NextGraphScanAt    *time.Time `json:"nextGraphScanAt" gorm:"index"`
	OperationalPause   string     `json:"operationalPause" gorm:"size:64"`
	OperationalDetails string     `json:"operationalDetails" gorm:"type:text"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type DiscoveryDomainBlacklistEntry struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Domain    string    `json:"domain" gorm:"uniqueIndex;not null;size:512"`
	Reason    string    `json:"reason" gorm:"type:text"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type DiscoveryDomainBlacklistMutation struct {
	Entry              DiscoveryDomainBlacklistEntry `json:"entry"`
	AffectedCandidates int64                         `json:"affectedCandidates"`
}

type DiscoverySiteEdge struct {
	ID              uint           `json:"id" gorm:"primaryKey"`
	EdgeKey         string         `json:"edgeKey" gorm:"uniqueIndex;not null;size:64"`
	FromSiteID      uint           `json:"fromSiteId" gorm:"index;not null"`
	ToSiteID        uint           `json:"toSiteId" gorm:"index;not null"`
	SourcePageURL   string         `json:"sourcePageUrl" gorm:"not null;size:2048"`
	AnchorText      string         `json:"anchorText" gorm:"size:1024"`
	RelationType    string         `json:"relationType" gorm:"index;not null;size:64"`
	DetectionRule   string         `json:"detectionRule" gorm:"index;size:64"`
	ContextSummary  string         `json:"contextSummary" gorm:"type:text"`
	EvidenceSummary string         `json:"evidenceSummary" gorm:"type:text"`
	Confidence      float64        `json:"confidence" gorm:"not null;default:0"`
	FirstSeenAt     time.Time      `json:"firstSeenAt" gorm:"not null"`
	LastSeenAt      time.Time      `json:"lastSeenAt" gorm:"index;not null"`
	Active          bool           `json:"active" gorm:"index;not null;default:true"`
	GraphDepth      int            `json:"graphDepth" gorm:"index;not null;default:0"`
	PendingReason   string         `json:"pendingReason" gorm:"size:64"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
	FromSite        *DiscoverySite `json:"-" gorm:"foreignKey:FromSiteID;constraint:OnDelete:CASCADE"`
	ToSite          *DiscoverySite `json:"-" gorm:"foreignKey:ToSiteID;constraint:OnDelete:CASCADE"`
}

type DiscoveryCandidateProvenance struct {
	ID              uint                `json:"id" gorm:"primaryKey"`
	ProvenanceKey   string              `json:"provenanceKey" gorm:"uniqueIndex;not null;size:64"`
	CandidateID     uint                `json:"candidateId" gorm:"index;not null"`
	SiteID          uint                `json:"siteId" gorm:"index;not null"`
	SourceID        *uint               `json:"sourceId" gorm:"index"`
	DiscoveryMethod string              `json:"discoveryMethod" gorm:"index;not null;size:64"`
	OriginalURL     string              `json:"originalUrl" gorm:"not null;size:2048"`
	SourcePageURL   string              `json:"sourcePageUrl" gorm:"size:2048"`
	FirstSeenAt     time.Time           `json:"firstSeenAt" gorm:"not null"`
	LastSeenAt      time.Time           `json:"lastSeenAt" gorm:"index;not null"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
	Candidate       *DiscoveryCandidate `json:"-" gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE"`
	Site            *DiscoverySite      `json:"-" gorm:"foreignKey:SiteID;constraint:OnDelete:CASCADE"`
	Source          *DiscoverySource    `json:"-" gorm:"foreignKey:SourceID;constraint:OnDelete:SET NULL"`
}

type DiscoveryFetchRun struct {
	ID             uint             `json:"id" gorm:"primaryKey"`
	JobID          string           `json:"jobId" gorm:"index;size:64"`
	SiteID         *uint            `json:"siteId" gorm:"index"`
	SourceID       uint             `json:"sourceId" gorm:"index;not null"`
	StartedAt      time.Time        `json:"startedAt" gorm:"index;not null"`
	FinishedAt     *time.Time       `json:"finishedAt"`
	Status         string           `json:"status" gorm:"index;not null;size:32"`
	HTTPStatus     int              `json:"httpStatus"`
	FinalURL       string           `json:"finalUrl" gorm:"size:2048"`
	ContentType    string           `json:"contentType" gorm:"size:255"`
	ETag           string           `json:"etag" gorm:"column:etag;size:1024"`
	LastModified   string           `json:"lastModified" gorm:"size:1024"`
	RobotsStatus   string           `json:"robotsStatus" gorm:"size:32"`
	NotModified    bool             `json:"notModified" gorm:"not null;default:false"`
	NewCount       int              `json:"newCount" gorm:"not null;default:0"`
	DuplicateCount int              `json:"duplicateCount" gorm:"not null;default:0"`
	FailureCount   int              `json:"failureCount" gorm:"not null;default:0"`
	ErrorCategory  string           `json:"errorCategory" gorm:"index;size:64"`
	ErrorSummary   string           `json:"errorSummary" gorm:"type:text"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
	Site           *DiscoverySite   `json:"-" gorm:"foreignKey:SiteID;constraint:OnDelete:SET NULL"`
	Source         *DiscoverySource `json:"-" gorm:"foreignKey:SourceID;constraint:OnDelete:CASCADE"`
}

type DiscoveryBackfillState struct {
	ID               uint           `json:"id" gorm:"primaryKey"`
	SiteID           uint           `json:"siteId" gorm:"uniqueIndex:idx_backfill_site_strategy;not null"`
	Strategy         string         `json:"strategy" gorm:"uniqueIndex:idx_backfill_site_strategy;not null;size:32"`
	Cursor           string         `json:"cursor" gorm:"type:text"`
	BatchNumber      uint           `json:"batchNumber" gorm:"not null;default:0"`
	Status           string         `json:"status" gorm:"index;not null;default:pending;size:32"`
	EarliestCovered  *time.Time     `json:"earliestCovered"`
	LatestCovered    *time.Time     `json:"latestCovered"`
	URLsSeen         uint           `json:"urlsSeen" gorm:"not null;default:0"`
	ArticlesFound    uint           `json:"articlesFound" gorm:"not null;default:0"`
	DuplicateCount   uint           `json:"duplicateCount" gorm:"not null;default:0"`
	FailureCount     uint           `json:"failureCount" gorm:"not null;default:0"`
	LastSuccessAt    *time.Time     `json:"lastSuccessAt"`
	LastBatchAt      *time.Time     `json:"lastBatchAt"`
	NextBatchAt      *time.Time     `json:"nextBatchAt" gorm:"index"`
	OwnerRequestedAt *time.Time     `json:"ownerRequestedAt" gorm:"index"`
	CompletionReason string         `json:"completionReason" gorm:"size:128"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	Site             *DiscoverySite `json:"-" gorm:"foreignKey:SiteID;constraint:OnDelete:CASCADE"`
}

type DiscoveryArticleAssessment struct {
	ID                 uint                `json:"id" gorm:"primaryKey"`
	CandidateID        uint                `json:"candidateId" gorm:"uniqueIndex:idx_assessment_version;not null"`
	ContentVersion     uint                `json:"contentVersion" gorm:"uniqueIndex:idx_assessment_version;not null"`
	Assessor           string              `json:"assessor" gorm:"uniqueIndex:idx_assessment_version;not null;size:64"`
	AssessorVersion    string              `json:"assessorVersion" gorm:"uniqueIndex:idx_assessment_version;not null;size:64"`
	PolicyVersion      string              `json:"policyVersion" gorm:"uniqueIndex:idx_assessment_version;not null;size:64"`
	InformationDensity float64             `json:"informationDensity"`
	Originality        float64             `json:"originality"`
	Completeness       float64             `json:"completeness"`
	Evidence           float64             `json:"evidence"`
	Readability        float64             `json:"readability"`
	Depth              float64             `json:"depth"`
	EvergreenValue     float64             `json:"evergreenValue"`
	OverallQuality     float64             `json:"overallQuality" gorm:"index"`
	Confidence         float64             `json:"confidence"`
	Reasons            string              `json:"reasons" gorm:"type:text"`
	CreatedAt          time.Time           `json:"createdAt"`
	Candidate          *DiscoveryCandidate `json:"-" gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE"`
}

type DiscoveryArticleContentVersion struct {
	ID             uint                `json:"id" gorm:"primaryKey"`
	CandidateID    uint                `json:"candidateId" gorm:"uniqueIndex:idx_candidate_content_version;not null"`
	ContentVersion uint                `json:"contentVersion" gorm:"uniqueIndex:idx_candidate_content_version;not null"`
	ContentHash    string              `json:"contentHash" gorm:"index;not null;size:128"`
	FinalURL       string              `json:"finalUrl" gorm:"size:2048"`
	CanonicalURL   string              `json:"canonicalUrl" gorm:"size:2048"`
	Title          string              `json:"title" gorm:"size:1024"`
	Summary        string              `json:"summary" gorm:"type:text"`
	Author         string              `json:"author" gorm:"size:255"`
	BodyText       string              `json:"bodyText" gorm:"type:text"`
	Language       string              `json:"language" gorm:"size:32"`
	WordCount      int                 `json:"wordCount" gorm:"not null;default:0"`
	PublishedAt    *time.Time          `json:"publishedAt"`
	FetchedAt      time.Time           `json:"fetchedAt" gorm:"not null"`
	CreatedAt      time.Time           `json:"createdAt"`
	Candidate      *DiscoveryCandidate `json:"-" gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE"`
}

type DiscoveryDuplicateCluster struct {
	ClusterID            string              `json:"clusterId" gorm:"primaryKey;size:128"`
	RepresentativeID     uint                `json:"representativeId" gorm:"index;not null"`
	MatchMethod          string              `json:"matchMethod" gorm:"index;not null;size:32"`
	RepresentativeReason string              `json:"representativeReason" gorm:"type:text"`
	MemberCount          uint                `json:"memberCount" gorm:"not null;default:1"`
	CreatedAt            time.Time           `json:"createdAt"`
	UpdatedAt            time.Time           `json:"updatedAt"`
	Representative       *DiscoveryCandidate `json:"-" gorm:"foreignKey:RepresentativeID;constraint:OnDelete:RESTRICT"`
}

type DiscoveryCandidateIdentity struct {
	ID          uint                `json:"id" gorm:"primaryKey"`
	CandidateID uint                `json:"candidateId" gorm:"uniqueIndex:idx_candidate_identity;index;not null"`
	Kind        string              `json:"kind" gorm:"uniqueIndex:idx_candidate_identity;index;not null;size:32"`
	IdentityKey string              `json:"identityKey" gorm:"uniqueIndex:idx_candidate_identity;index;not null;size:128"`
	Value       string              `json:"value" gorm:"type:text;not null"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
	Candidate   *DiscoveryCandidate `json:"-" gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE"`
}

type DiscoveryDuplicateReviewSignal struct {
	ID                   uint                `json:"id" gorm:"primaryKey"`
	CandidateID          uint                `json:"candidateId" gorm:"index;not null"`
	ReporterUserID       uint                `json:"reporterUserId" gorm:"index;not null"`
	RecommendationItemID uint                `json:"recommendationItemId" gorm:"index;not null"`
	Status               string              `json:"status" gorm:"index;not null;default:pending;size:32"`
	CreatedAt            time.Time           `json:"createdAt"`
	UpdatedAt            time.Time           `json:"updatedAt"`
	Candidate            *DiscoveryCandidate `json:"-" gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE"`
}

type UserCandidateState struct {
	ID              uint                `json:"id" gorm:"primaryKey"`
	UserID          uint                `json:"userId" gorm:"uniqueIndex:idx_user_candidate_state;not null"`
	CandidateID     uint                `json:"candidateId" gorm:"uniqueIndex:idx_user_candidate_state;not null"`
	FirstExposedAt  *time.Time          `json:"firstExposedAt"`
	LastExposedAt   *time.Time          `json:"lastExposedAt" gorm:"index"`
	ExposureCount   uint                `json:"exposureCount" gorm:"not null;default:0"`
	OpenedAt        *time.Time          `json:"openedAt"`
	ReadAt          *time.Time          `json:"readAt"`
	DeepReadAt      *time.Time          `json:"deepReadAt"`
	ArchivedAt      *time.Time          `json:"archivedAt"`
	CurrentFeedback string              `json:"currentFeedback" gorm:"index;size:32"`
	FeedbackSetAt   *time.Time          `json:"feedbackSetAt"`
	FeedbackRevoked *time.Time          `json:"feedbackRevoked"`
	MigratedFrom    string              `json:"migratedFrom" gorm:"size:64"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
	Candidate       *DiscoveryCandidate `json:"-" gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE"`
}

type DiscoveryLegacyCandidateStateReview struct {
	ID           uint                `json:"id" gorm:"primaryKey"`
	CandidateID  uint                `json:"candidateId" gorm:"uniqueIndex;not null"`
	LegacyStatus string              `json:"legacyStatus" gorm:"index;not null;size:32"`
	Resolution   string              `json:"resolution" gorm:"index;not null;default:pending;size:32"`
	Notes        string              `json:"notes" gorm:"type:text"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
	Candidate    *DiscoveryCandidate `json:"-" gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE"`
}

// DiscoverySiteOperationalStats keeps named scheduling and capacity signals.
// It intentionally has no aggregate source quality or reputation score.
type DiscoverySiteOperationalStats struct {
	ID                       uint           `json:"id" gorm:"primaryKey"`
	SiteID                   uint           `json:"siteId" gorm:"uniqueIndex;not null"`
	IndependentInboundSites  uint           `json:"independentInboundSites"`
	FetchAttempts            uint           `json:"fetchAttempts"`
	FetchSuccesses           uint           `json:"fetchSuccesses"`
	NotModifiedFetches       uint           `json:"notModifiedFetches"`
	ParseSuccesses           uint           `json:"parseSuccesses"`
	CandidateCount           uint           `json:"candidateCount"`
	EligibleCandidateCount   uint           `json:"eligibleCandidateCount"`
	DuplicateCandidateCount  uint           `json:"duplicateCandidateCount"`
	ExtractedCandidateCount  uint           `json:"extractedCandidateCount"`
	PositiveFeedbackArticles uint           `json:"positiveFeedbackArticles"`
	BackfillURLsSeen         uint           `json:"backfillUrlsSeen"`
	BackfillArticlesFound    uint           `json:"backfillArticlesFound"`
	LastComputedAt           time.Time      `json:"lastComputedAt" gorm:"index"`
	CreatedAt                time.Time      `json:"createdAt"`
	UpdatedAt                time.Time      `json:"updatedAt"`
	Site                     *DiscoverySite `json:"-" gorm:"foreignKey:SiteID;constraint:OnDelete:CASCADE"`
}

type DiscoverySourceScheduleDecision struct {
	ID                    uint             `json:"id" gorm:"primaryKey"`
	SourceID              uint             `json:"sourceId" gorm:"uniqueIndex;not null"`
	SiteID                *uint            `json:"siteId" gorm:"index"`
	Basis                 string           `json:"basis" gorm:"index;not null;size:32"`
	BaseIntervalSeconds   int64            `json:"baseIntervalSeconds"`
	ChosenIntervalSeconds int64            `json:"chosenIntervalSeconds"`
	Explanation           string           `json:"explanation" gorm:"type:text"`
	NextDueAt             time.Time        `json:"nextDueAt" gorm:"index"`
	ComputedAt            time.Time        `json:"computedAt" gorm:"index"`
	CreatedAt             time.Time        `json:"createdAt"`
	UpdatedAt             time.Time        `json:"updatedAt"`
	Source                *DiscoverySource `json:"-" gorm:"foreignKey:SourceID;constraint:OnDelete:CASCADE"`
}

func V3Models() []interface{} {
	return []interface{}{
		&DiscoveryDomainBlacklistEntry{},
		&DiscoverySite{},
		&DiscoverySiteEdge{},
		&DiscoveryCandidateProvenance{},
		&DiscoveryFetchRun{},
		&DiscoveryBackfillState{},
		&DiscoveryArticleContentVersion{},
		&DiscoveryDuplicateCluster{},
		&DiscoveryCandidateIdentity{},
		&DiscoveryDuplicateReviewSignal{},
		&DiscoveryArticleAssessment{},
		&UserCandidateState{},
		&DiscoveryLegacyCandidateStateReview{},
		&DiscoverySiteOperationalStats{},
		&DiscoverySourceScheduleDecision{},
	}
}
