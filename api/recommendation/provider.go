package recommendation

import (
	"context"
	"time"
)

type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type EnrichmentProvider interface {
	Enrich(ctx context.Context, input EnrichmentInput) (EnrichmentResult, error)
}

type ArticleAssessmentProvider interface {
	AssessArticle(ctx context.Context, input ArticleAssessmentInput) (ArticleAssessmentResult, error)
}

type RerankProvider interface {
	Rerank(ctx context.Context, input RerankInput) (RerankResult, error)
}

type EnrichmentInput struct {
	CandidateID uint
	URL         string
	Title       string
	Summary     string
	BodyText    string
	PublishedAt *time.Time
}

type EnrichmentResult struct {
	Summary         string
	Topics          []string
	Entities        []string
	ContentType     string
	ContentStyle    string
	Language        string
	QualityScore    float64
	DepthScore      float64
	SpamProbability float64
	Model           string
	PromptVersion   string
}

type ArticleAssessmentInput struct {
	CandidateID uint
	Title       string
	BodyText    string
}

type ArticleAssessmentResult struct {
	QualityScore           int      `json:"qualityScore"`
	DepthScore             int      `json:"depthScore"`
	EvergreenScore         int      `json:"evergreenScore"`
	Reasons                []string `json:"reasons"`
	Summary                string   `json:"summary"`
	Keywords               []string `json:"keywords"`
	Model                  string   `json:"-"`
	PromptVersion          string   `json:"-"`
	EvidenceTokens         int      `json:"-"`
	OriginalEvidenceTokens int      `json:"-"`
	EvidenceTruncated      bool     `json:"-"`
}

type RerankInput struct {
	UserID          uint
	RequestedCount  int
	Candidates      []RerankCandidate
	UserProfileHint string
}

type RerankCandidate struct {
	CandidateID  uint
	Title        string
	Summary      string
	Topics       []string
	Source       string
	PublishedAt  *time.Time
	QualityScore float64
	DepthScore   float64
}

type RerankResult struct {
	Items         []RerankItem
	Model         string
	PromptVersion string
}

type RerankItem struct {
	CandidateID uint
	Rank        int
	Reason      string
	Confidence  float64
}

type DigestSummaryGenerator interface {
	GenerateDigestSummary(ctx context.Context, input DigestSummaryInput) (DigestSummaryOutput, error)
}

type DigestSummaryInput struct {
	Date  string
	Items []DigestSummaryItem
}

type DigestSummaryItem struct {
	Rank    int      `json:"rank"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Source  string   `json:"source"`
	Topics  []string `json:"topics"`
	Reason  string   `json:"reason"`
}

type DigestSummaryOutput struct {
	Overview      string   `json:"overview"`
	Highlights    []string `json:"highlights"`
	Topics        []string `json:"topics"`
	Model         string   `json:"-"`
	PromptVersion string   `json:"-"`
}
