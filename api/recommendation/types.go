package recommendation

import (
	"errors"
)

// types.go 定义推荐日状态、反馈动作、错误值与快照结构。

const (
	RecommendationDayStatusMissing      = "missing"
	RecommendationDayStatusDraft        = "draft"
	RecommendationDayStatusPublished    = "published"
	RecommendationDayStatusSupplemented = "supplemented"
	RecommendationDayStatusFailed       = "failed"

	// Compatibility names remain source-compatible while persisted lifecycle
	// values use the immutable v3 terminology.
	RecommendationDayStatusPending   = RecommendationDayStatusDraft
	RecommendationDayStatusGenerated = RecommendationDayStatusPublished

	RecommendationFeedbackValuable      = "valuable"
	RecommendationFeedbackNotInterested = "not_interested"
	RecommendationFeedbackDuplicate     = "duplicate"
	RecommendationFeedbackTooRepetitive = "too_repetitive"
	RecommendationFeedbackDeepRead      = "deep_read"
	RecommendationFeedbackLowValue      = "low_value"
	RecommendationFeedbackBlock         = "block"
	RecommendationFeedbackBlockSource   = "block_source"
	RecommendationFeedbackReduceTopic   = "reduce_topic"
	RecommendationFeedbackReduceStyle   = "reduce_style"

	UserBlockRuleTopic  = "topic"
	UserBlockRuleSource = "source"
	UserBlockRuleStyle  = "style"

	RecommendationEnrichmentStatusPending = "pending"
	RecommendationEnrichmentStatusReady   = "ready"
	RecommendationEnrichmentStatusFailed  = "failed"
)

var (
	ErrInvalidRecommendationFeedback = errors.New("invalid recommendation feedback action")
	ErrInvalidBlockRule              = errors.New("invalid block rule")
	ErrDuplicateRecommendationItem   = errors.New("recommendation item already exists for this user")
	ErrRecommendationDayImmutable    = errors.New("published recommendation day is immutable")
)

// RecommendationDaySnapshot 是某日日报及其条目的只读快照。
type RecommendationDaySnapshot struct {
	Day   *RecommendationDay   `json:"day"`
	Items []RecommendationItem `json:"items"`
}

// RecommendationBlockTarget 描述一条反馈要屏蔽的主题、来源或风格。
type RecommendationBlockTarget struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type recommendationFeedbackMetadata struct {
	BlockTargets []RecommendationBlockTarget `json:"blockTargets,omitempty"`
}

type recommendationCandidateScore struct {
	Candidate         DiscoveryCandidate
	Topics            []string
	SourceHost        string
	Author            string
	PoolTags          []string
	PoolType          string
	Exploration       bool
	ExplorationReason string
	ContentUpdated    bool
	CooldownRepeat    bool
	RetrievalScore    float64
	RerankScore       float64
	RerankRank        int
	FinalScore        float64
	Reason            string
}
