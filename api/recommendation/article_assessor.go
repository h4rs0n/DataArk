package recommendation

import (
	"DataArk/config"
	"DataArk/discovery"
	"context"
	"fmt"
	"strings"
)

type EnrichmentArticleAssessor struct {
	Provider EnrichmentProvider
	Model    string
}

func (assessor EnrichmentArticleAssessor) Name() string { return "openai_compatible" }
func (assessor EnrichmentArticleAssessor) Version() string {
	if model := strings.TrimSpace(assessor.Model); model != "" {
		return model
	}
	return "configured"
}
func (EnrichmentArticleAssessor) PolicyVersion() string { return "article-quality-v1+llm-v1" }

func (assessor EnrichmentArticleAssessor) Assess(ctx context.Context, input discovery.ArticleAssessmentInput) (discovery.ArticleAssessmentResult, error) {
	if assessor.Provider == nil {
		return discovery.ArticleAssessmentResult{}, fmt.Errorf("missing optional article assessment provider")
	}
	result, err := assessor.Provider.Enrich(ctx, EnrichmentInput{
		Title: input.Title, BodyText: input.BodyText, PublishedAt: input.PublishedAt,
	})
	if err != nil {
		return discovery.ArticleAssessmentResult{}, err
	}
	quality := clampScore(result.QualityScore)
	depth := clampScore(result.DepthScore)
	confidence := 0.75
	if strings.TrimSpace(result.Model) == "" {
		confidence = 0.6
	}
	return discovery.ArticleAssessmentResult{
		InformationDensity: quality, Originality: quality, Completeness: (quality + depth) / 2,
		Evidence: quality, Readability: quality, Depth: depth, EvergreenValue: (quality + depth) / 2,
		OverallQuality: quality, Confidence: confidence,
		Reasons: []string{fmt.Sprintf("optional OpenAI-compatible article-only assessment model=%s prompt=%s", result.Model, result.PromptVersion)},
	}, nil
}

func ConfiguredArticleAssessor() discovery.ArticleAssessor {
	if strings.TrimSpace(config.LLMCHATMODEL) == "" {
		return nil
	}
	return EnrichmentArticleAssessor{Provider: configuredOpenAICompatibleProvider(), Model: config.LLMCHATMODEL}
}
