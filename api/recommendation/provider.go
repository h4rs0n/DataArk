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
