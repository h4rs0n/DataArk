package assessment

import (
	"DataArk/articlevalue"
	"context"
	"fmt"
	"time"
)

type BatchRun struct {
	Active        bool        `json:"active" gorm:"-"`
	LeaseUntil    *time.Time  `json:"-"`
	PausedAt      *time.Time  `json:"pausedAt,omitempty"`
	ErrorCode     int         `json:"errorCode,omitempty"`
	Error         string      `json:"error,omitempty"`
	ResumeStatus  string      `json:"-"`
	ID            uint        `json:"id" gorm:"primaryKey"`
	ActiveKey     *int        `json:"-" gorm:"uniqueIndex"`
	Status        string      `json:"status"`
	BaseURL       string      `json:"-"`
	Model         string      `json:"model"`
	PolicyVersion string      `json:"policyVersion"`
	HighWater     int64       `json:"-"`
	Cursor        int64       `json:"-"`
	CreatedAt     time.Time   `json:"createdAt"`
	FinishedAt    *time.Time  `json:"finishedAt,omitempty"`
	Counts        BatchCounts `json:"counts" gorm:"-"`
}

func (BatchRun) TableName() string { return "assessment_batch_runs" }

type BatchCounts struct {
	Total      int64 `json:"total"`
	Returned   int64 `json:"returned"`
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Succeeded  int64 `json:"succeeded"`
	Failed     int64 `json:"failed"`
	Reused     int64 `json:"reused"`
	Skipped    int64 `json:"skipped"`
}
type Batch struct {
	InputJSONL     []byte     `json:"-" gorm:"column:input_jsonl"`
	FailureCount   int        `json:"-"`
	ID             uint       `json:"id" gorm:"primaryKey"`
	RunID          uint       `json:"runId" gorm:"index"`
	UUID           string     `json:"-" gorm:"uniqueIndex"`
	Model          string     `json:"model"`
	ResponseMode   string     `json:"responseMode" gorm:"not null;default:json_object"`
	Phase          string     `json:"phase"`
	ProviderStatus string     `json:"providerStatus"`
	InputFileID    string     `json:"inputFileId,omitempty"`
	OutputFileID   string     `json:"-"`
	ErrorFileID    string     `json:"-"`
	RemoteID       string     `json:"remoteId,omitempty"`
	Digest         string     `json:"-"`
	FileBytes      int64      `json:"-"`
	Total          int        `json:"total"`
	Completed      int        `json:"completed"`
	Failed         int        `json:"failed"`
	Error          string     `json:"error,omitempty"`
	NextCheckAt    time.Time  `json:"-" gorm:"index"`
	CreatedAt      time.Time  `json:"createdAt"`
	SubmittedAt    *time.Time `json:"submittedAt,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	CleanedAt      *time.Time `json:"-"`
}

func (Batch) TableName() string { return "assessment_batches" }

type BatchItem struct {
	ID               uint       `json:"id" gorm:"primaryKey"`
	RunID            uint       `json:"runId" gorm:"index"`
	BatchID          *uint      `json:"batchId,omitempty" gorm:"index"`
	JobID            string     `json:"-"`
	ReturnedAt       *time.Time `json:"returnedAt,omitempty"`
	RestoredJobID    string     `json:"-"`
	MaterialID       uint       `json:"materialId"`
	VersionID        uint       `json:"-"`
	ContentVersion   uint       `json:"contentVersion"`
	CandidateID      uint       `json:"candidateId"`
	CustomID         string     `json:"-" gorm:"uniqueIndex"`
	ActiveIdentity   *string    `json:"-" gorm:"uniqueIndex"`
	Status           string     `json:"status" gorm:"index"`
	AssessmentID     *uint      `json:"assessmentId,omitempty"`
	EvidenceTokens   int        `json:"-"`
	PromptTokens     int        `json:"promptTokens"`
	CompletionTokens int        `json:"completionTokens"`
	ReasoningTokens  int        `json:"reasoningTokens"`
	TotalTokens      int        `json:"totalTokens"`
	Error            string     `json:"error,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	FinishedAt       *time.Time `json:"finishedAt,omitempty"`
}

func (BatchItem) TableName() string { return "assessment_batch_items" }

type batchIdentity struct {
	Model  string
	Policy string
}

func (batchIdentity) Name() string      { return "bailian_batch" }
func (a batchIdentity) Version() string { return a.Model }
func (a batchIdentity) PolicyVersion() string {
	if a.Policy != "" {
		return a.Policy
	}
	return articlevalue.PolicyVersion
}
func (batchIdentity) Assess(context.Context, ArticleAssessmentInput) (ArticleAssessmentResult, error) {
	return ArticleAssessmentResult{}, fmt.Errorf("Batch assessments must be started through the assessment queue")
}
