package assessmenteval

import (
	"DataArk/articlevalue"
	"DataArk/discovery"
	"DataArk/observability"
	"DataArk/recommendation"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BlindPassDelay = 72 * time.Hour

	WorkflowStatusPassOne          = "pass_one"
	WorkflowStatusWaitingPassTwo   = "waiting_pass_two"
	WorkflowStatusPassTwo          = "pass_two"
	WorkflowStatusAdjudication     = "adjudication"
	WorkflowStatusHumanComplete    = "human_complete"
	WorkflowStatusEvaluating       = "evaluating"
	WorkflowStatusComplete         = "complete"
	WorkflowStatusEvaluationFailed = "evaluation_failed"
)

var workflowActiveStatuses = []string{
	WorkflowStatusPassOne,
	WorkflowStatusWaitingPassTwo,
	WorkflowStatusPassTwo,
	WorkflowStatusAdjudication,
	WorkflowStatusHumanComplete,
	WorkflowStatusEvaluating,
	WorkflowStatusEvaluationFailed,
}

type ArticleAssessmentWorkflowRun struct {
	ID                    uint       `json:"id" gorm:"primaryKey"`
	Seed                  string     `json:"seed" gorm:"not null;size:160"`
	ManifestDigest        string     `json:"manifestDigest" gorm:"not null;size:128"`
	PolicyVersion         string     `json:"policyVersion" gorm:"not null;size:64"`
	Status                string     `json:"status" gorm:"index;not null;size:32"`
	CreatedBy             uint       `json:"createdBy" gorm:"index;not null"`
	PassOneCompletedAt    *time.Time `json:"passOneCompletedAt"`
	PassTwoStartedAt      *time.Time `json:"passTwoStartedAt"`
	PassTwoCompletedAt    *time.Time `json:"passTwoCompletedAt"`
	HumanCompletedAt      *time.Time `json:"humanCompletedAt"`
	EvaluationGeneration  int        `json:"evaluationGeneration" gorm:"not null;default:0"`
	EvaluationStartedAt   *time.Time `json:"evaluationStartedAt"`
	EvaluationCompletedAt *time.Time `json:"evaluationCompletedAt"`
	Model                 string     `json:"model" gorm:"size:255"`
	PromptVersion         string     `json:"promptVersion" gorm:"size:128"`
	EvaluationError       string     `json:"evaluationError" gorm:"type:text"`
	ReportJSON            string     `json:"-" gorm:"type:text"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

func (ArticleAssessmentWorkflowRun) TableName() string {
	return "article_assessment_workflow_runs"
}

type ArticleAssessmentWorkflowItem struct {
	ID                   uint   `gorm:"primaryKey"`
	RunID                uint   `gorm:"uniqueIndex:idx_assessment_workflow_item;index;not null"`
	Position             int    `gorm:"not null"`
	PassTwoPosition      *int   `gorm:"index"`
	AdjudicationPosition *int   `gorm:"index"`
	SampleID             string `gorm:"uniqueIndex:idx_assessment_workflow_item;not null;size:64"`
	CandidateID          uint   `gorm:"index;not null"`
	ContentVersionID     uint   `gorm:"index;not null"`
	ContentVersion       uint   `gorm:"not null"`
	ContentHash          string `gorm:"not null;size:128"`
	Host                 string `gorm:"size:255"`
	Language             string `gorm:"size:32"`
	BodyCharacters       int    `gorm:"not null"`
	Stratum              string `gorm:"index;not null;size:64"`
	BaselineAssessor     string `gorm:"size:128"`
	BaselineQuality      int    `gorm:"not null"`
	BaselineDepth        int    `gorm:"not null"`
	BaselineEvergreen    int    `gorm:"not null"`
	RuleQuality          int    `gorm:"not null"`
	RuleDepth            int    `gorm:"not null"`
	RuleEvergreen        int    `gorm:"not null"`
	CreatedAt            time.Time
}

func (ArticleAssessmentWorkflowItem) TableName() string {
	return "article_assessment_workflow_items"
}

type ArticleAssessmentWorkflowLabel struct {
	ID              uint   `gorm:"primaryKey"`
	RunID           uint   `gorm:"uniqueIndex:idx_assessment_workflow_label;index;not null"`
	SampleID        string `gorm:"uniqueIndex:idx_assessment_workflow_label;not null;size:64"`
	Pass            int    `gorm:"uniqueIndex:idx_assessment_workflow_label;not null"`
	Quality         int    `gorm:"not null"`
	Depth           int    `gorm:"not null"`
	Evergreen       int    `gorm:"not null"`
	Reason          string `gorm:"type:text"`
	Genre           string `gorm:"size:32"`
	ExtractionBad   bool   `gorm:"not null;default:false"`
	Unjudgeable     bool   `gorm:"not null;default:false"`
	DurationSeconds int    `gorm:"not null;default:0"`
	LabeledBy       uint   `gorm:"index;not null"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (ArticleAssessmentWorkflowLabel) TableName() string {
	return "article_assessment_workflow_labels"
}

type ArticleAssessmentWorkflowScore struct {
	ID                     uint   `gorm:"primaryKey"`
	RunID                  uint   `gorm:"uniqueIndex:idx_assessment_workflow_score;index;not null"`
	Generation             int    `gorm:"uniqueIndex:idx_assessment_workflow_score;not null"`
	SampleID               string `gorm:"uniqueIndex:idx_assessment_workflow_score;not null;size:64"`
	ModelRun               int    `gorm:"uniqueIndex:idx_assessment_workflow_score;not null"`
	CandidateID            uint   `gorm:"index;not null"`
	Quality                int    `gorm:"not null"`
	Depth                  int    `gorm:"not null"`
	Evergreen              int    `gorm:"not null"`
	ReasonsJSON            string `gorm:"type:text"`
	EvidenceTokens         int    `gorm:"not null;default:0"`
	OriginalEvidenceTokens int    `gorm:"not null;default:0"`
	EvidenceTruncated      bool   `gorm:"not null;default:false"`
	DurationMilliseconds   int64  `gorm:"not null;default:0"`
	Error                  string `gorm:"type:text"`
	CreatedAt              time.Time
}

func (ArticleAssessmentWorkflowScore) TableName() string {
	return "article_assessment_workflow_scores"
}

type ArticleAssessmentWorkflowCall struct {
	ID                     uint   `gorm:"primaryKey"`
	RunID                  uint   `gorm:"index;not null"`
	Generation             int    `gorm:"index;not null"`
	CandidateID            uint   `gorm:"index;not null"`
	Status                 string `gorm:"not null;size:32"`
	ErrorType              string `gorm:"size:64"`
	Model                  string `gorm:"size:255"`
	ResponseMode           string `gorm:"size:32"`
	Attempt                int    `gorm:"not null;default:0"`
	EvidenceTokens         int    `gorm:"not null;default:0"`
	OriginalEvidenceTokens int    `gorm:"not null;default:0"`
	EvidenceTruncated      bool   `gorm:"not null;default:false"`
	DurationMilliseconds   int64  `gorm:"not null;default:0"`
	UsageAvailable         bool   `gorm:"not null;default:false"`
	PromptTokens           int    `gorm:"not null;default:0"`
	CompletionTokens       int    `gorm:"not null;default:0"`
	ReasoningTokens        int    `gorm:"not null;default:0"`
	CachedTokens           int    `gorm:"not null;default:0"`
	TotalTokens            int    `gorm:"not null;default:0"`
	CreatedAt              time.Time
}

func (ArticleAssessmentWorkflowCall) TableName() string {
	return "article_assessment_workflow_calls"
}

func WorkflowModels() []interface{} {
	return []interface{}{
		&ArticleAssessmentWorkflowRun{},
		&ArticleAssessmentWorkflowItem{},
		&ArticleAssessmentWorkflowLabel{},
		&ArticleAssessmentWorkflowScore{},
		&ArticleAssessmentWorkflowCall{},
	}
}

type WorkflowProgress struct {
	Total   int `json:"total"`
	Labeled int `json:"labeled"`
}

type WorkflowSummary struct {
	Exists               bool             `json:"exists"`
	RunID                uint             `json:"runId,omitempty"`
	Status               string           `json:"status,omitempty"`
	PolicyVersion        string           `json:"policyVersion,omitempty"`
	CreatedAt            time.Time        `json:"createdAt,omitempty"`
	PassOne              WorkflowProgress `json:"passOne"`
	PassTwo              WorkflowProgress `json:"passTwo"`
	Adjudication         WorkflowProgress `json:"adjudication"`
	NextPassAvailableAt  *time.Time       `json:"nextPassAvailableAt,omitempty"`
	CanAdvance           bool             `json:"canAdvance"`
	CanEvaluate          bool             `json:"canEvaluate"`
	EvaluationGeneration int              `json:"evaluationGeneration"`
	EvaluationProgress   WorkflowProgress `json:"evaluationProgress"`
	Model                string           `json:"model,omitempty"`
	PromptVersion        string           `json:"promptVersion,omitempty"`
	EvaluationError      string           `json:"evaluationError,omitempty"`
	Report               *Report          `json:"report,omitempty"`
}

type WorkflowLabelInput struct {
	Scores          *AxisScores `json:"scores"`
	Reason          string      `json:"reason"`
	Genre           string      `json:"genre"`
	ExtractionBad   bool        `json:"extractionBad"`
	Unjudgeable     bool        `json:"unjudgeable"`
	DurationSeconds int         `json:"durationSeconds"`
}

type WorkflowItemView struct {
	RunID          uint                `json:"runId"`
	Pass           int                 `json:"pass"`
	Position       int                 `json:"position"`
	Total          int                 `json:"total"`
	SampleID       string              `json:"sampleId"`
	Title          string              `json:"title"`
	BodyText       string              `json:"bodyText"`
	Language       string              `json:"language"`
	BodyCharacters int                 `json:"bodyCharacters"`
	Label          *WorkflowLabelInput `json:"label,omitempty"`
}

func StartWorkflow(database *gorm.DB, userID uint, now time.Time) (WorkflowSummary, error) {
	if database == nil || userID == 0 {
		return WorkflowSummary{}, errors.New("article assessment workflow database and owner are required")
	}
	var active int64
	if err := database.Model(&ArticleAssessmentWorkflowRun{}).Where("status IN ?", workflowActiveStatuses).Count(&active).Error; err != nil {
		return WorkflowSummary{}, err
	}
	if active > 0 {
		return WorkflowSummary{}, errors.New("an article assessment workflow is already active")
	}
	records, err := LoadCandidateRecords(database)
	if err != nil {
		return WorkflowSummary{}, err
	}
	seed := fmt.Sprintf("article-value-v3-gold-v1:%s", now.UTC().Format("20060102T150405.000000000Z"))
	manifest, err := BuildManifest(records, seed, now.UTC())
	if err != nil {
		return WorkflowSummary{}, err
	}
	run := ArticleAssessmentWorkflowRun{
		Seed: seed, ManifestDigest: manifest.Digest, PolicyVersion: articlevalue.PolicyVersion,
		Status: WorkflowStatusPassOne, CreatedBy: userID, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	err = database.Transaction(func(transaction *gorm.DB) error {
		if createErr := transaction.Create(&run).Error; createErr != nil {
			return createErr
		}
		items := make([]ArticleAssessmentWorkflowItem, 0, len(manifest.Items))
		for position, item := range manifest.Items {
			items = append(items, workflowItemFromManifest(run.ID, position, item, now.UTC()))
		}
		return transaction.CreateInBatches(items, 100).Error
	})
	if err != nil {
		return WorkflowSummary{}, err
	}
	return GetWorkflowSummary(database, run.ID, now)
}

func workflowItemFromManifest(runID uint, position int, item ManifestItem, now time.Time) ArticleAssessmentWorkflowItem {
	return ArticleAssessmentWorkflowItem{
		RunID: runID, Position: position, SampleID: item.SampleID,
		CandidateID: item.CandidateID, ContentVersionID: item.ContentVersionID, ContentVersion: item.ContentVersion,
		ContentHash: item.ContentHash, Host: item.Host, Language: item.Language, BodyCharacters: item.BodyCharacters,
		Stratum: item.Stratum, BaselineAssessor: item.BaselineAssessor,
		BaselineQuality: item.BaselineScores.Quality, BaselineDepth: item.BaselineScores.Depth, BaselineEvergreen: item.BaselineScores.Evergreen,
		RuleQuality: item.RuleScores.Quality, RuleDepth: item.RuleScores.Depth, RuleEvergreen: item.RuleScores.Evergreen,
		CreatedAt: now,
	}
}

func LatestWorkflowSummary(database *gorm.DB, now time.Time) (WorkflowSummary, error) {
	if database == nil {
		return WorkflowSummary{}, errors.New("article assessment workflow database is unavailable")
	}
	var run ArticleAssessmentWorkflowRun
	err := database.Order("id DESC").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkflowSummary{PassOne: WorkflowProgress{Total: GoldSampleCount}}, nil
	}
	if err != nil {
		return WorkflowSummary{}, err
	}
	return GetWorkflowSummary(database, run.ID, now)
}

func GetWorkflowSummary(database *gorm.DB, runID uint, now time.Time) (WorkflowSummary, error) {
	run, err := loadWorkflowRun(database, runID)
	if err != nil {
		return WorkflowSummary{}, err
	}
	summary := WorkflowSummary{
		Exists: true, RunID: run.ID, Status: run.Status, PolicyVersion: run.PolicyVersion, CreatedAt: run.CreatedAt,
		PassOne: WorkflowProgress{Total: GoldSampleCount}, EvaluationGeneration: run.EvaluationGeneration,
		Model: run.Model, PromptVersion: run.PromptVersion, EvaluationError: run.EvaluationError,
	}
	var passCounts []struct {
		Pass  int
		Count int
	}
	if err := database.Model(&ArticleAssessmentWorkflowLabel{}).Select("pass, count(*) AS count").Where("run_id = ?", run.ID).Group("pass").Scan(&passCounts).Error; err != nil {
		return WorkflowSummary{}, err
	}
	for _, count := range passCounts {
		switch count.Pass {
		case 1:
			summary.PassOne.Labeled = count.Count
		case 2:
			summary.PassTwo.Labeled = count.Count
		case 3:
			summary.Adjudication.Labeled = count.Count
		}
	}
	var passTwoTotal int64
	if err := database.Model(&ArticleAssessmentWorkflowItem{}).Where("run_id = ? AND pass_two_position IS NOT NULL", run.ID).Count(&passTwoTotal).Error; err != nil {
		return WorkflowSummary{}, err
	}
	summary.PassTwo.Total = int(passTwoTotal)
	var adjudicationTotal int64
	if err := database.Model(&ArticleAssessmentWorkflowItem{}).Where("run_id = ? AND adjudication_position IS NOT NULL", run.ID).Count(&adjudicationTotal).Error; err != nil {
		return WorkflowSummary{}, err
	}
	summary.Adjudication.Total = int(adjudicationTotal)
	if run.PassOneCompletedAt != nil {
		available := run.PassOneCompletedAt.Add(BlindPassDelay)
		summary.NextPassAvailableAt = &available
	}
	switch run.Status {
	case WorkflowStatusPassOne:
		summary.CanAdvance = summary.PassOne.Labeled == summary.PassOne.Total
	case WorkflowStatusWaitingPassTwo:
		summary.CanAdvance = summary.NextPassAvailableAt != nil && !now.Before(*summary.NextPassAvailableAt)
	case WorkflowStatusPassTwo:
		summary.CanAdvance = summary.PassTwo.Total == 30 && summary.PassTwo.Labeled == summary.PassTwo.Total
	case WorkflowStatusAdjudication:
		summary.CanAdvance = summary.Adjudication.Total > 0 && summary.Adjudication.Labeled == summary.Adjudication.Total
	case WorkflowStatusHumanComplete, WorkflowStatusEvaluationFailed:
		summary.CanEvaluate = true
	}
	if run.EvaluationGeneration > 0 {
		summary.EvaluationProgress.Total = GoldSampleCount * 2
		var evaluated int64
		if err := database.Model(&ArticleAssessmentWorkflowScore{}).Where("run_id = ? AND generation = ?", run.ID, run.EvaluationGeneration).Count(&evaluated).Error; err != nil {
			return WorkflowSummary{}, err
		}
		summary.EvaluationProgress.Labeled = int(evaluated)
	}
	if strings.TrimSpace(run.ReportJSON) != "" {
		var report Report
		if err := json.Unmarshal([]byte(run.ReportJSON), &report); err != nil {
			return WorkflowSummary{}, fmt.Errorf("decode stored article assessment report: %w", err)
		}
		summary.Report = &report
		if run.Status == WorkflowStatusComplete && !report.ActivationReady {
			summary.CanEvaluate = true
		}
	}
	return summary, nil
}

func loadWorkflowRun(database *gorm.DB, runID uint) (ArticleAssessmentWorkflowRun, error) {
	if database == nil || runID == 0 {
		return ArticleAssessmentWorkflowRun{}, gorm.ErrRecordNotFound
	}
	var run ArticleAssessmentWorkflowRun
	if err := database.First(&run, runID).Error; err != nil {
		return run, err
	}
	return run, nil
}

func GetWorkflowItem(database *gorm.DB, runID uint, pass, position int) (WorkflowItemView, error) {
	run, err := loadWorkflowRun(database, runID)
	if err != nil {
		return WorkflowItemView{}, err
	}
	if pass < 1 || pass > 3 || position < 0 {
		return WorkflowItemView{}, errors.New("invalid workflow item position")
	}
	query := database.Where("run_id = ?", run.ID)
	order := "position"
	switch pass {
	case 1:
		if run.Status != WorkflowStatusPassOne {
			return WorkflowItemView{}, errors.New("blind pass one is frozen")
		}
	case 2:
		if run.Status != WorkflowStatusPassTwo {
			return WorkflowItemView{}, errors.New("blind pass two is not editable")
		}
		query = query.Where("pass_two_position IS NOT NULL")
		order = "pass_two_position"
	case 3:
		if run.Status != WorkflowStatusAdjudication {
			return WorkflowItemView{}, errors.New("adjudication is not available")
		}
		query = query.Where("adjudication_position IS NOT NULL")
		order = "adjudication_position"
	}
	var total int64
	if err := query.Model(&ArticleAssessmentWorkflowItem{}).Count(&total).Error; err != nil {
		return WorkflowItemView{}, err
	}
	if position >= int(total) {
		return WorkflowItemView{}, gorm.ErrRecordNotFound
	}
	var item ArticleAssessmentWorkflowItem
	if err := query.Order(order).Offset(position).Limit(1).First(&item).Error; err != nil {
		return WorkflowItemView{}, err
	}
	var content discovery.DiscoveryArticleContentVersion
	if err := database.First(&content, item.ContentVersionID).Error; err != nil {
		return WorkflowItemView{}, err
	}
	if content.CandidateID != item.CandidateID || content.ContentVersion != item.ContentVersion || content.ContentHash != item.ContentHash {
		return WorkflowItemView{}, errors.New("immutable assessment content version no longer matches the workflow")
	}
	view := WorkflowItemView{
		RunID: run.ID, Pass: pass, Position: position, Total: int(total), SampleID: item.SampleID,
		Title: strings.TrimSpace(content.Title), BodyText: strings.TrimSpace(content.BodyText), Language: item.Language, BodyCharacters: item.BodyCharacters,
	}
	var label ArticleAssessmentWorkflowLabel
	err = database.Where("run_id = ? AND sample_id = ? AND pass = ?", run.ID, item.SampleID, pass).First(&label).Error
	if err == nil {
		view.Label = workflowLabelInput(label)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkflowItemView{}, err
	}
	return view, nil
}

func workflowLabelInput(label ArticleAssessmentWorkflowLabel) *WorkflowLabelInput {
	return &WorkflowLabelInput{
		Scores: &AxisScores{Quality: label.Quality, Depth: label.Depth, Evergreen: label.Evergreen},
		Reason: label.Reason, Genre: label.Genre, ExtractionBad: label.ExtractionBad,
		Unjudgeable: label.Unjudgeable, DurationSeconds: label.DurationSeconds,
	}
}

func SaveWorkflowLabel(database *gorm.DB, runID, userID uint, pass int, sampleID string, input WorkflowLabelInput, now time.Time) (WorkflowSummary, error) {
	run, err := loadWorkflowRun(database, runID)
	if err != nil {
		return WorkflowSummary{}, err
	}
	expectedStatus := map[int]string{1: WorkflowStatusPassOne, 2: WorkflowStatusPassTwo, 3: WorkflowStatusAdjudication}[pass]
	if expectedStatus == "" || run.Status != expectedStatus {
		return WorkflowSummary{}, errors.New("this blind-label pass is not editable")
	}
	sampleID = strings.TrimSpace(sampleID)
	var item ArticleAssessmentWorkflowItem
	query := database.Where("run_id = ? AND sample_id = ?", run.ID, sampleID)
	if pass == 2 {
		query = query.Where("pass_two_position IS NOT NULL")
	} else if pass == 3 {
		query = query.Where("adjudication_position IS NOT NULL")
	}
	if err := query.First(&item).Error; err != nil {
		return WorkflowSummary{}, err
	}
	if err := validateWorkflowLabel(input); err != nil {
		return WorkflowSummary{}, err
	}
	scores := AxisScores{}
	if input.Scores != nil {
		scores = *input.Scores
	}
	duration := input.DurationSeconds
	if duration < 0 {
		duration = 0
	}
	if duration > 24*60*60 {
		duration = 24 * 60 * 60
	}
	label := ArticleAssessmentWorkflowLabel{
		RunID: run.ID, SampleID: item.SampleID, Pass: pass,
		Quality: scores.Quality, Depth: scores.Depth, Evergreen: scores.Evergreen,
		Reason: strings.TrimSpace(input.Reason), Genre: strings.TrimSpace(input.Genre),
		ExtractionBad: input.ExtractionBad, Unjudgeable: input.Unjudgeable,
		DurationSeconds: duration, LabeledBy: userID, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	err = database.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "run_id"}, {Name: "sample_id"}, {Name: "pass"}},
		DoUpdates: clause.AssignmentColumns([]string{"quality", "depth", "evergreen", "reason", "genre", "extraction_bad", "unjudgeable", "duration_seconds", "labeled_by", "updated_at"}),
	}).Create(&label).Error
	if err != nil {
		return WorkflowSummary{}, err
	}
	return GetWorkflowSummary(database, run.ID, now)
}

func validateWorkflowLabel(input WorkflowLabelInput) error {
	if input.Unjudgeable {
		return nil
	}
	if input.Scores == nil || !validScores(*input.Scores) {
		return errors.New("quality, depth, and evergreen must be integers from 0 through 100")
	}
	if strings.TrimSpace(input.Reason) == "" || len([]rune(strings.TrimSpace(input.Reason))) > 300 {
		return errors.New("a concise human reason of at most 300 characters is required")
	}
	validGenres := map[string]bool{"analysis": true, "tutorial": true, "reference": true, "essay": true, "news": true, "release": true, "personal-update": true, "other": true}
	if !validGenres[strings.TrimSpace(input.Genre)] {
		return errors.New("a supported article genre is required")
	}
	return nil
}

func AdvanceWorkflow(database *gorm.DB, runID uint, now time.Time) (WorkflowSummary, error) {
	run, err := loadWorkflowRun(database, runID)
	if err != nil {
		return WorkflowSummary{}, err
	}
	summary, err := GetWorkflowSummary(database, run.ID, now)
	if err != nil {
		return WorkflowSummary{}, err
	}
	if !summary.CanAdvance {
		if run.Status == WorkflowStatusWaitingPassTwo && summary.NextPassAvailableAt != nil {
			return summary, fmt.Errorf("blind pass two is unavailable until %s", summary.NextPassAvailableAt.UTC().Format(time.RFC3339))
		}
		return summary, errors.New("the current workflow phase is incomplete")
	}
	now = now.UTC()
	switch run.Status {
	case WorkflowStatusPassOne:
		err = database.Model(&run).Updates(map[string]interface{}{
			"status": WorkflowStatusWaitingPassTwo, "pass_one_completed_at": now, "updated_at": now,
		}).Error
	case WorkflowStatusWaitingPassTwo:
		err = startWorkflowPassTwo(database, run, now)
	case WorkflowStatusPassTwo:
		err = completeWorkflowPassTwo(database, run, now)
	case WorkflowStatusAdjudication:
		err = database.Model(&run).Updates(map[string]interface{}{
			"status": WorkflowStatusHumanComplete, "human_completed_at": now, "updated_at": now,
		}).Error
	default:
		err = errors.New("the current workflow phase cannot advance")
	}
	if err != nil {
		return summary, err
	}
	return GetWorkflowSummary(database, run.ID, now)
}

func startWorkflowPassTwo(database *gorm.DB, run ArticleAssessmentWorkflowRun, now time.Time) error {
	manifest, err := loadWorkflowManifest(database, run)
	if err != nil {
		return err
	}
	selected, err := SelectPassTwoSampleIDs(manifest)
	if err != nil {
		return err
	}
	return database.Transaction(func(transaction *gorm.DB) error {
		for position, sampleID := range selected {
			if result := transaction.Model(&ArticleAssessmentWorkflowItem{}).Where("run_id = ? AND sample_id = ?", run.ID, sampleID).Update("pass_two_position", position); result.Error != nil || result.RowsAffected != 1 {
				if result.Error != nil {
					return result.Error
				}
				return fmt.Errorf("blind pass-two sample %q is missing", sampleID)
			}
		}
		return transaction.Model(&run).Updates(map[string]interface{}{
			"status": WorkflowStatusPassTwo, "pass_two_started_at": now, "updated_at": now,
		}).Error
	})
}

func completeWorkflowPassTwo(database *gorm.DB, run ArticleAssessmentWorkflowRun, now time.Time) error {
	first, err := loadWorkflowLabelMap(database, run.ID, 1)
	if err != nil {
		return err
	}
	second, err := loadWorkflowLabelMap(database, run.ID, 2)
	if err != nil {
		return err
	}
	var repeated []ArticleAssessmentWorkflowItem
	if err := database.Where("run_id = ? AND pass_two_position IS NOT NULL", run.ID).Order("pass_two_position").Find(&repeated).Error; err != nil {
		return err
	}
	conflicts := make([]string, 0)
	for _, item := range repeated {
		left, leftOK := first[item.SampleID]
		right, rightOK := second[item.SampleID]
		if leftOK && rightOK && !left.Unjudgeable && !right.Unjudgeable && scoresConflict(left.Scores, right.Scores) {
			conflicts = append(conflicts, item.SampleID)
		}
	}
	return database.Transaction(func(transaction *gorm.DB) error {
		for position, sampleID := range conflicts {
			if err := transaction.Model(&ArticleAssessmentWorkflowItem{}).Where("run_id = ? AND sample_id = ?", run.ID, sampleID).Update("adjudication_position", position).Error; err != nil {
				return err
			}
		}
		updates := map[string]interface{}{"pass_two_completed_at": now, "updated_at": now}
		if len(conflicts) == 0 {
			updates["status"] = WorkflowStatusHumanComplete
			updates["human_completed_at"] = now
		} else {
			updates["status"] = WorkflowStatusAdjudication
		}
		return transaction.Model(&run).Updates(updates).Error
	})
}

func SelectPassTwoSampleIDs(manifest Manifest) ([]string, error) {
	core := make([]ManifestItem, 0, CoreSampleCount)
	stress := make([]ManifestItem, 0, StressSampleCount)
	for _, item := range manifest.Items {
		switch {
		case strings.HasPrefix(item.Stratum, "core:"):
			core = append(core, item)
		case strings.HasPrefix(item.Stratum, "stress:"):
			stress = append(stress, item)
		}
	}
	sortManifestItems(core, manifest.Seed, "pass-two-core")
	sortManifestItems(stress, manifest.Seed, "pass-two-stress")
	if len(core) < 20 || len(stress) < 10 {
		return nil, errors.New("manifest does not contain enough core and stress samples for pass two")
	}
	selected := append(append([]ManifestItem{}, core[:20]...), stress[:10]...)
	sortManifestItems(selected, manifest.Seed, "pass-two-order")
	ids := make([]string, 0, len(selected))
	for _, item := range selected {
		ids = append(ids, item.SampleID)
	}
	return ids, nil
}

func sortManifestItems(items []ManifestItem, seed, purpose string) {
	sort.Slice(items, func(left, right int) bool {
		leftDigest := sha256.Sum256([]byte(seed + "\x00" + purpose + "\x00" + items[left].SampleID))
		rightDigest := sha256.Sum256([]byte(seed + "\x00" + purpose + "\x00" + items[right].SampleID))
		return hex.EncodeToString(leftDigest[:]) < hex.EncodeToString(rightDigest[:])
	})
}

func loadWorkflowLabelMap(database *gorm.DB, runID uint, pass int) (map[string]Label, error) {
	set, err := loadWorkflowLabelSet(database, runID, pass)
	if err != nil {
		return nil, err
	}
	return indexLabels(set.Labels)
}

func loadWorkflowLabelSet(database *gorm.DB, runID uint, pass int) (LabelSet, error) {
	run, err := loadWorkflowRun(database, runID)
	if err != nil {
		return LabelSet{}, err
	}
	var rows []ArticleAssessmentWorkflowLabel
	if err := database.Where("run_id = ? AND pass = ?", runID, pass).Order("sample_id").Find(&rows).Error; err != nil {
		return LabelSet{}, err
	}
	set := LabelSet{Version: LabelVersion, ManifestDigest: run.ManifestDigest, Pass: pass, StartedAt: run.CreatedAt}
	if pass == 1 {
		set.CompletedAt = timeValue(run.PassOneCompletedAt)
	} else if pass == 2 {
		set.StartedAt, set.CompletedAt = timeValue(run.PassTwoStartedAt), timeValue(run.PassTwoCompletedAt)
	} else {
		set.StartedAt, set.CompletedAt = timeValue(run.PassTwoCompletedAt), timeValue(run.HumanCompletedAt)
	}
	for _, row := range rows {
		set.Labels = append(set.Labels, Label{
			SampleID: row.SampleID, Scores: AxisScores{Quality: row.Quality, Depth: row.Depth, Evergreen: row.Evergreen},
			Reason: row.Reason, Genre: row.Genre, ExtractionBad: row.ExtractionBad,
			Unjudgeable: row.Unjudgeable, DurationSeconds: row.DurationSeconds,
		})
	}
	return set, nil
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func loadWorkflowManifest(database *gorm.DB, run ArticleAssessmentWorkflowRun) (Manifest, error) {
	var items []ArticleAssessmentWorkflowItem
	if err := database.Where("run_id = ?", run.ID).Order("position").Find(&items).Error; err != nil {
		return Manifest{}, err
	}
	if len(items) != GoldSampleCount {
		return Manifest{}, fmt.Errorf("workflow contains %d items, want %d", len(items), GoldSampleCount)
	}
	ids := make([]uint, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ContentVersionID)
	}
	var versions []discovery.DiscoveryArticleContentVersion
	if err := database.Where("id IN ?", ids).Find(&versions).Error; err != nil {
		return Manifest{}, err
	}
	byID := make(map[uint]discovery.DiscoveryArticleContentVersion, len(versions))
	for _, version := range versions {
		byID[version.ID] = version
	}
	manifest := Manifest{Version: ManifestVersion, Digest: run.ManifestDigest, Seed: run.Seed, CreatedAt: run.CreatedAt, Items: make([]ManifestItem, 0, len(items))}
	for _, item := range items {
		version, exists := byID[item.ContentVersionID]
		if !exists || version.CandidateID != item.CandidateID || version.ContentVersion != item.ContentVersion || version.ContentHash != item.ContentHash {
			return Manifest{}, fmt.Errorf("immutable content version for sample %q is unavailable", item.SampleID)
		}
		manifest.Items = append(manifest.Items, ManifestItem{
			SampleID: item.SampleID, CandidateID: item.CandidateID, ContentVersionID: item.ContentVersionID,
			ContentVersion: item.ContentVersion, ContentHash: item.ContentHash, Title: strings.TrimSpace(version.Title), BodyText: strings.TrimSpace(version.BodyText),
			Language: item.Language, BodyCharacters: item.BodyCharacters, Host: item.Host, Stratum: item.Stratum,
			BaselineAssessor: item.BaselineAssessor,
			BaselineScores:   AxisScores{Quality: item.BaselineQuality, Depth: item.BaselineDepth, Evergreen: item.BaselineEvergreen},
			RuleScores:       AxisScores{Quality: item.RuleQuality, Depth: item.RuleDepth, Evergreen: item.RuleEvergreen},
		})
	}
	digest, err := ManifestDigest(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if digest != run.ManifestDigest {
		return Manifest{}, errors.New("stored workflow manifest digest mismatch")
	}
	return manifest, nil
}

func StartEvaluation(database *gorm.DB, runID uint, provider recommendation.OpenAICompatibleProvider, now time.Time) (WorkflowSummary, error) {
	if strings.TrimSpace(provider.ChatModel) == "" {
		return WorkflowSummary{}, errors.New("LLM chat model is not configured")
	}
	run, err := loadWorkflowRun(database, runID)
	if err != nil {
		return WorkflowSummary{}, err
	}
	if run.Status != WorkflowStatusHumanComplete && run.Status != WorkflowStatusEvaluationFailed && run.Status != WorkflowStatusComplete {
		return WorkflowSummary{}, errors.New("human labels must be complete before model evaluation")
	}
	generation := run.EvaluationGeneration + 1
	now = now.UTC()
	result := database.Model(&ArticleAssessmentWorkflowRun{}).
		Where("id = ? AND status IN ?", run.ID, []string{WorkflowStatusHumanComplete, WorkflowStatusEvaluationFailed, WorkflowStatusComplete}).
		Updates(map[string]interface{}{
			"status": WorkflowStatusEvaluating, "evaluation_generation": generation,
			"evaluation_started_at": now, "evaluation_completed_at": nil,
			"model": strings.TrimSpace(provider.ChatModel), "prompt_version": articlevalue.PromptVersion,
			"evaluation_error": "", "report_json": "", "updated_at": now,
		})
	if result.Error != nil {
		return WorkflowSummary{}, result.Error
	}
	if result.RowsAffected != 1 {
		return WorkflowSummary{}, errors.New("article assessment evaluation is already running")
	}
	go func() {
		if evaluationErr := runWorkflowEvaluation(database, run.ID, generation, provider, time.Now().UTC()); evaluationErr != nil {
			message := compactEvaluationError(evaluationErr)
			log.Printf("article assessment workflow %d evaluation failed: %s", run.ID, message)
			_ = database.Model(&ArticleAssessmentWorkflowRun{}).Where("id = ? AND evaluation_generation = ?", run.ID, generation).Updates(map[string]interface{}{
				"status": WorkflowStatusEvaluationFailed, "evaluation_error": message,
				"evaluation_completed_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
			}).Error
		}
	}()
	return GetWorkflowSummary(database, run.ID, now)
}

func runWorkflowEvaluation(database *gorm.DB, runID uint, generation int, provider recommendation.OpenAICompatibleProvider, now time.Time) error {
	run, err := loadWorkflowRun(database, runID)
	if err != nil {
		return err
	}
	manifest, err := loadWorkflowManifest(database, run)
	if err != nil {
		return err
	}
	var observerMu sync.Mutex
	var observerErr error
	provider.CallObserver = func(event observability.Event) {
		call := workflowCallFromEvent(runID, generation, event, time.Now().UTC())
		if createErr := database.Create(&call).Error; createErr != nil {
			observerMu.Lock()
			observerErr = errors.Join(observerErr, createErr)
			observerMu.Unlock()
		}
	}
	type evaluationJob struct {
		item ManifestItem
		run  int
	}
	jobs := make(chan evaluationJob)
	var wait sync.WaitGroup
	var scoreMu sync.Mutex
	var scoreErr error
	for worker := 0; worker < 2; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for job := range jobs {
				started := time.Now()
				result, callErr := provider.AssessArticle(context.Background(), recommendation.ArticleAssessmentInput{
					CandidateID: job.item.CandidateID, Title: job.item.Title, BodyText: job.item.BodyText,
				})
				score := ArticleAssessmentWorkflowScore{
					RunID: runID, Generation: generation, SampleID: job.item.SampleID, ModelRun: job.run,
					CandidateID: job.item.CandidateID, DurationMilliseconds: time.Since(started).Milliseconds(), CreatedAt: time.Now().UTC(),
				}
				if callErr != nil {
					score.Error = compactEvaluationError(callErr)
				} else {
					capped := articlevalue.ApplyEvidenceCaps(articlevalue.Scores{
						Quality: float64(result.QualityScore) / 100, Depth: float64(result.DepthScore) / 100, Evergreen: float64(result.EvergreenScore) / 100,
					}, result.OriginalEvidenceTokens)
					score.Quality = int(math.Round(capped.Quality * 100))
					score.Depth = int(math.Round(capped.Depth * 100))
					score.Evergreen = int(math.Round(capped.Evergreen * 100))
					reasons, _ := json.Marshal(result.Reasons)
					score.ReasonsJSON = string(reasons)
					score.EvidenceTokens = result.EvidenceTokens
					score.OriginalEvidenceTokens = result.OriginalEvidenceTokens
					score.EvidenceTruncated = result.EvidenceTruncated
				}
				if createErr := database.Create(&score).Error; createErr != nil {
					scoreMu.Lock()
					scoreErr = errors.Join(scoreErr, createErr)
					scoreMu.Unlock()
				}
			}
		}()
	}
	for modelRun := 1; modelRun <= 2; modelRun++ {
		for _, item := range manifest.Items {
			jobs <- evaluationJob{item: item, run: modelRun}
		}
	}
	close(jobs)
	wait.Wait()
	observerMu.Lock()
	capturedObserverErr := observerErr
	observerMu.Unlock()
	if err := errors.Join(scoreErr, capturedObserverErr); err != nil {
		return err
	}
	passOne, err := loadWorkflowLabelSet(database, runID, 1)
	if err != nil {
		return err
	}
	passTwo, err := loadWorkflowLabelSet(database, runID, 2)
	if err != nil {
		return err
	}
	var adjudication *LabelSet
	var conflictCount int64
	if err := database.Model(&ArticleAssessmentWorkflowItem{}).Where("run_id = ? AND adjudication_position IS NOT NULL", runID).Count(&conflictCount).Error; err != nil {
		return err
	}
	if conflictCount > 0 {
		labels, loadErr := loadWorkflowLabelSet(database, runID, 3)
		if loadErr != nil {
			return loadErr
		}
		adjudication = &labels
	}
	scoreSet, err := loadWorkflowScoreSet(database, run, generation)
	if err != nil {
		return err
	}
	callLog, err := workflowCallLog(database, runID, generation)
	if err != nil {
		return err
	}
	report, err := BuildReport(manifest, passOne, passTwo, adjudication, scoreSet, bytes.NewReader(callLog), now)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return err
	}
	completed := time.Now().UTC()
	return database.Model(&ArticleAssessmentWorkflowRun{}).Where("id = ? AND evaluation_generation = ?", runID, generation).Updates(map[string]interface{}{
		"status": WorkflowStatusComplete, "report_json": string(payload), "evaluation_error": "",
		"evaluation_completed_at": completed, "updated_at": completed,
	}).Error
}

func workflowCallFromEvent(runID uint, generation int, event observability.Event, now time.Time) ArticleAssessmentWorkflowCall {
	call := ArticleAssessmentWorkflowCall{
		RunID: runID, Generation: generation, CandidateID: event.CandidateID, Status: event.Status,
		ErrorType: event.ErrorType, Model: event.LLMModel, ResponseMode: event.LLMResponseMode, Attempt: event.LLMAttempt,
		EvidenceTokens: event.LLMEvidenceTokens, OriginalEvidenceTokens: event.LLMOriginalEvidenceTokens,
		EvidenceTruncated: event.LLMEvidenceTruncated, DurationMilliseconds: event.LLMDuration, CreatedAt: now,
	}
	if event.LLMUsage != nil {
		call.UsageAvailable = event.LLMUsage.Available
		call.PromptTokens = event.LLMUsage.PromptTokens
		call.CompletionTokens = event.LLMUsage.CompletionTokens
		call.ReasoningTokens = event.LLMUsage.ReasoningTokens
		call.CachedTokens = event.LLMUsage.CachedTokens
		call.TotalTokens = event.LLMUsage.TotalTokens
	}
	return call
}

func loadWorkflowScoreSet(database *gorm.DB, run ArticleAssessmentWorkflowRun, generation int) (ScoreSet, error) {
	var rows []ArticleAssessmentWorkflowScore
	if err := database.Where("run_id = ? AND generation = ?", run.ID, generation).Order("model_run, sample_id").Find(&rows).Error; err != nil {
		return ScoreSet{}, err
	}
	set := ScoreSet{Version: ScoreVersion, ManifestDigest: run.ManifestDigest, Model: run.Model, PromptVersion: articlevalue.PromptVersion, CreatedAt: time.Now().UTC()}
	for _, row := range rows {
		var reasons []string
		_ = json.Unmarshal([]byte(row.ReasonsJSON), &reasons)
		set.Records = append(set.Records, ScoreRecord{
			SampleID: row.SampleID, CandidateID: row.CandidateID, Run: row.ModelRun,
			Scores: AxisScores{Quality: row.Quality, Depth: row.Depth, Evergreen: row.Evergreen}, Reasons: reasons,
			EvidenceTokens: row.EvidenceTokens, OriginalEvidenceTokens: row.OriginalEvidenceTokens,
			EvidenceTruncated: row.EvidenceTruncated, DurationMilliseconds: row.DurationMilliseconds, Error: row.Error,
		})
	}
	return set, nil
}

func workflowCallLog(database *gorm.DB, runID uint, generation int) ([]byte, error) {
	var calls []ArticleAssessmentWorkflowCall
	if err := database.Where("run_id = ? AND generation = ?", runID, generation).Order("id").Find(&calls).Error; err != nil {
		return nil, err
	}
	var output bytes.Buffer
	for _, call := range calls {
		event := llmLogEvent{Event: "llm_call", CandidateID: call.CandidateID, Status: call.Status, LLMStage: "article_assessment", Duration: call.DurationMilliseconds}
		event.Usage = &struct {
			Available        bool `json:"available"`
			PromptTokens     int  `json:"prompt_tokens"`
			CompletionTokens int  `json:"completion_tokens"`
			ReasoningTokens  int  `json:"reasoning_tokens"`
			CachedTokens     int  `json:"cached_tokens"`
			TotalTokens      int  `json:"total_tokens"`
		}{call.UsageAvailable, call.PromptTokens, call.CompletionTokens, call.ReasoningTokens, call.CachedTokens, call.TotalTokens}
		payload, _ := json.Marshal(event)
		fmt.Fprintf(&output, "dataark_event %s\n", payload)
	}
	return output.Bytes(), nil
}

func RecoverInterruptedEvaluations(database *gorm.DB, now time.Time) error {
	if database == nil {
		return nil
	}
	return database.Model(&ArticleAssessmentWorkflowRun{}).Where("status = ?", WorkflowStatusEvaluating).Updates(map[string]interface{}{
		"status":                  WorkflowStatusEvaluationFailed,
		"evaluation_error":        "server restarted during model evaluation; retry from the workflow page",
		"evaluation_completed_at": now.UTC(), "updated_at": now.UTC(),
	}).Error
}

func compactEvaluationError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(value)
	if len(runes) > 300 {
		value = string(runes[:300])
	}
	return value
}
