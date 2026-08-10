package assessmenteval

import "time"

const (
	ManifestVersion = "article-assessment-gold-v1"
	LabelVersion    = "article-assessment-labels-v1"
	ScoreVersion    = "article-assessment-scores-v1"
	ReportVersion   = "article-assessment-report-v1"
)

type AxisScores struct {
	Quality   int `json:"quality"`
	Depth     int `json:"depth"`
	Evergreen int `json:"evergreen"`
}

type CandidateRecord struct {
	CandidateID      uint
	ContentVersionID uint
	ContentVersion   uint
	ContentHash      string
	Host             string
	Title            string
	BodyText         string
	Language         string
	ActiveAssessor   string
	ActiveScores     AxisScores
	RuleScores       AxisScores
}

type Manifest struct {
	Version   string         `json:"version"`
	Digest    string         `json:"digest"`
	Seed      string         `json:"seed"`
	CreatedAt time.Time      `json:"createdAt"`
	Items     []ManifestItem `json:"items"`
}

type ManifestItem struct {
	SampleID         string     `json:"sampleId"`
	CandidateID      uint       `json:"candidateId"`
	ContentVersionID uint       `json:"contentVersionId"`
	ContentVersion   uint       `json:"contentVersion"`
	ContentHash      string     `json:"contentHash"`
	Title            string     `json:"title"`
	BodyText         string     `json:"bodyText"`
	Language         string     `json:"language"`
	BodyCharacters   int        `json:"bodyCharacters"`
	Host             string     `json:"host"`
	Stratum          string     `json:"stratum"`
	BaselineAssessor string     `json:"baselineAssessor"`
	BaselineScores   AxisScores `json:"baselineScores"`
	RuleScores       AxisScores `json:"ruleScores"`
}

type LabelSet struct {
	Version        string    `json:"version"`
	ManifestDigest string    `json:"manifestDigest"`
	Pass           int       `json:"pass"`
	StartedAt      time.Time `json:"startedAt"`
	CompletedAt    time.Time `json:"completedAt"`
	Labels         []Label   `json:"labels"`
}

type Label struct {
	SampleID        string     `json:"sampleId"`
	Scores          AxisScores `json:"scores"`
	Reason          string     `json:"reason"`
	Genre           string     `json:"genre"`
	ExtractionBad   bool       `json:"extractionBad"`
	Unjudgeable     bool       `json:"unjudgeable"`
	DurationSeconds int        `json:"durationSeconds"`
}

type ScoreSet struct {
	Version        string        `json:"version"`
	ManifestDigest string        `json:"manifestDigest"`
	Model          string        `json:"model"`
	PromptVersion  string        `json:"promptVersion"`
	CreatedAt      time.Time     `json:"createdAt"`
	Records        []ScoreRecord `json:"records"`
}

type ScoreRecord struct {
	SampleID               string     `json:"sampleId"`
	CandidateID            uint       `json:"candidateId"`
	Run                    int        `json:"run"`
	Scores                 AxisScores `json:"scores"`
	Reasons                []string   `json:"reasons,omitempty"`
	EvidenceTokens         int        `json:"evidenceTokens,omitempty"`
	OriginalEvidenceTokens int        `json:"originalEvidenceTokens,omitempty"`
	EvidenceTruncated      bool       `json:"evidenceTruncated,omitempty"`
	DurationMilliseconds   int64      `json:"durationMilliseconds"`
	Error                  string     `json:"error,omitempty"`
}

type AxisMetrics struct {
	Count         int     `json:"count"`
	Spearman      float64 `json:"spearman"`
	Kendall       float64 `json:"kendall"`
	MAE           float64 `json:"mae"`
	BandAgreement float64 `json:"bandAgreement"`
}

type ComparisonMetrics struct {
	Quality               AxisMetrics `json:"quality"`
	Depth                 AxisMetrics `json:"depth"`
	Evergreen             AxisMetrics `json:"evergreen"`
	TopQuintileHitRate    float64     `json:"topQuintileHitRate"`
	BottomQuintileHitRate float64     `json:"bottomQuintileHitRate"`
	EligibleRecall        float64     `json:"eligibleRecall"`
	LowQualityRejection   float64     `json:"lowQualityRejection"`
	QualitySaturation     float64     `json:"qualitySaturation"`
}

type ProtocolMetrics struct {
	Calls                int     `json:"calls"`
	SuccessfulCalls      int     `json:"successfulCalls"`
	ValidOutputRate      float64 `json:"validOutputRate"`
	PromptTokenP95       int     `json:"promptTokenP95"`
	MaximumPromptTokens  int     `json:"maximumPromptTokens"`
	CompletionTokens     int     `json:"completionTokens"`
	ReasoningTokens      int     `json:"reasoningTokens"`
	CachedTokens         int     `json:"cachedTokens"`
	TotalTokens          int     `json:"totalTokens"`
	DurationMilliseconds int64   `json:"durationMilliseconds"`
}

type Report struct {
	Version             string            `json:"version"`
	ManifestDigest      string            `json:"manifestDigest"`
	CreatedAt           time.Time         `json:"createdAt"`
	HumanConsistency    ComparisonMetrics `json:"humanConsistency"`
	ModelAll            ComparisonMetrics `json:"modelAll"`
	ModelCore           ComparisonMetrics `json:"modelCore"`
	BaselineCore        ComparisonMetrics `json:"baselineCore"`
	ModelRepeat         ComparisonMetrics `json:"modelRepeat"`
	Protocol            ProtocolMetrics   `json:"protocol"`
	Conflicts           []string          `json:"conflicts"`
	UnresolvedConflicts int               `json:"unresolvedConflicts"`
	ExcludedSamples     int               `json:"excludedSamples"`
	ActivationChecks    map[string]bool   `json:"activationChecks"`
	ActivationReady     bool              `json:"activationReady"`
}
