package recommendation

import (
	"DataArk/config"
	"context"
	"strings"
	"time"
)

// EmbeddingProvider 为候选正文生成向量。
type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// RerankProvider 对已选出的日报候选做 LLM 重排。
type RerankProvider interface {
	Rerank(ctx context.Context, input RerankInput) (RerankResult, error)
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

// DigestSummaryGenerator 为已发布日报生成用户可见摘要。
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

// ConfiguredRecommendationReranker 返回生产 reranker；未配置 LLM 时返回 nil。
func ConfiguredRecommendationReranker() RerankProvider {
	if strings.TrimSpace(config.LLMCHATMODEL) == "" {
		return nil
	}
	return configuredOpenAICompatibleProvider()
}

// ConfiguredOpenAICompatibleProvider 返回生产评估用的 OpenAI 兼容提供者，不通过 HTTP 暴露凭证。
func ConfiguredOpenAICompatibleProvider() OpenAICompatibleProvider {
	return configuredOpenAICompatibleProvider()
}

func configuredOpenAICompatibleProvider() OpenAICompatibleProvider {
	timeout, err := time.ParseDuration(strings.TrimSpace(config.LLMTIMEOUT))
	if err != nil || timeout <= 0 {
		timeout = 30 * time.Second
	}
	return OpenAICompatibleProvider{
		BaseURL:        config.LLMBASEURL,
		APIKey:         config.LLMAPIKEY,
		ChatModel:      config.LLMCHATMODEL,
		EmbeddingModel: config.LLMEMBEDDINGMODEL,
		Timeout:        timeout,
	}
}
