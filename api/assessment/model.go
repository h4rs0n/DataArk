package assessment

import (
	"DataArk/discovery"
	"DataArk/observability"
	"time"
)

// ArticleAssessment 是一篇候选在某一内容版本、评估器与策略下的不可变行。
// 表名保持 discovery_article_assessments，避免迁表与外键抖动。
type ArticleAssessment struct {
	ID                 uint                          `json:"id" gorm:"primaryKey"`
	CandidateID        uint                          `json:"candidateId" gorm:"uniqueIndex:idx_assessment_version;not null"`
	ContentVersion     uint                          `json:"contentVersion" gorm:"uniqueIndex:idx_assessment_version;not null"`
	Assessor           string                        `json:"assessor" gorm:"uniqueIndex:idx_assessment_version;not null;size:64"`
	AssessorVersion    string                        `json:"assessorVersion" gorm:"uniqueIndex:idx_assessment_version;not null;size:64"`
	PolicyVersion      string                        `json:"policyVersion" gorm:"uniqueIndex:idx_assessment_version;not null;size:64"`
	InformationDensity float64                       `json:"informationDensity"`
	Originality        float64                       `json:"originality"`
	Completeness       float64                       `json:"completeness"`
	Evidence           float64                       `json:"evidence"`
	Readability        float64                       `json:"readability"`
	Depth              float64                       `json:"depth"`
	EvergreenValue     float64                       `json:"evergreenValue"`
	OverallQuality     float64                       `json:"overallQuality" gorm:"index"`
	Confidence         float64                       `json:"confidence"`
	Reasons            string                        `json:"reasons" gorm:"type:text"`
	Summary            string                        `json:"summary" gorm:"type:text"`
	Keywords           string                        `json:"keywords" gorm:"type:text"`
	CreatedAt          time.Time                     `json:"createdAt"`
	Candidate          *discovery.DiscoveryCandidate `json:"-" gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE"`
}

func (ArticleAssessment) TableName() string { return "discovery_article_assessments" }

// LLMCall 只存安全观测字段，禁止写入 prompt 或模型正文。
type LLMCall struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	CandidateID      uint      `json:"candidateId" gorm:"index;not null;default:0"`
	Stage            string    `json:"stage" gorm:"index;not null;size:64"`
	Status           string    `json:"status" gorm:"index;not null;size:32"`
	Attempt          int       `json:"attempt" gorm:"not null;default:0"`
	DurationMS       int64     `json:"durationMs" gorm:"not null;default:0"`
	PromptTokens     int       `json:"promptTokens" gorm:"not null;default:0"`
	CompletionTokens int       `json:"completionTokens" gorm:"not null;default:0"`
	ReasoningTokens  int       `json:"reasoningTokens" gorm:"not null;default:0"`
	CachedTokens     int       `json:"cachedTokens" gorm:"not null;default:0"`
	TotalTokens      int       `json:"totalTokens" gorm:"not null;default:0"`
	EvidenceTokens   int       `json:"evidenceTokens" gorm:"not null;default:0"`
	ResponseMode     string    `json:"responseMode" gorm:"size:32"`
	ErrorType        string    `json:"errorType" gorm:"size:64"`
	CreatedAt        time.Time `json:"createdAt" gorm:"index"`
}

func (LLMCall) TableName() string { return "assessment_llm_calls" }

func V3Models() []interface{} {
	return []interface{}{&ArticleAssessment{}, &LLMCall{}}
}

func persistLLMCallEvent(event observability.Event) {
	if db == nil {
		return
	}
	usage := observability.LLMUsage{}
	if event.LLMUsage != nil {
		usage = *event.LLMUsage
	}
	row := LLMCall{
		CandidateID: event.CandidateID, Stage: event.LLMStage, Status: event.Status,
		Attempt: event.LLMAttempt, DurationMS: event.LLMDuration,
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		ReasoningTokens: usage.ReasoningTokens, CachedTokens: usage.CachedTokens,
		TotalTokens: usage.TotalTokens, EvidenceTokens: event.LLMEvidenceTokens,
		ResponseMode: event.LLMResponseMode, ErrorType: event.ErrorType,
		CreatedAt: event.OccurredAt,
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	_ = db.Create(&row).Error
}
