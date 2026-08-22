package assessment

import (
	"DataArk/articlevalue"
	"DataArk/config"
	"DataArk/llm"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type ArticleChatProvider interface {
	AssessArticle(context.Context, ChatAssessmentInput) (ChatAssessmentResult, error)
}

type EnrichmentArticleAssessor struct {
	Provider ArticleChatProvider
	Model    string
	Mode     string
}

func (assessor EnrichmentArticleAssessor) Name() string { return "openai_compatible" }
func (assessor EnrichmentArticleAssessor) Version() string {
	if model := strings.TrimSpace(assessor.Model); model != "" {
		return model
	}
	return "configured"
}
func (EnrichmentArticleAssessor) PolicyVersion() string { return articlevalue.PolicyVersion }

func (assessor EnrichmentArticleAssessor) ShouldActivateAssessment() bool {
	mode := strings.ToLower(strings.TrimSpace(assessor.Mode))
	return mode == "" || mode == "active"
}

func (assessor EnrichmentArticleAssessor) Assess(ctx context.Context, input ArticleAssessmentInput) (ArticleAssessmentResult, error) {
	if assessor.Provider == nil {
		return ArticleAssessmentResult{}, fmt.Errorf("missing optional article assessment provider")
	}
	release, err := acquireArticleAssessmentSlot(ctx)
	if err != nil {
		return ArticleAssessmentResult{}, err
	}
	defer release()
	result, err := assessor.Provider.AssessArticle(ctx, ChatAssessmentInput{
		CandidateID: input.CandidateID, Title: input.Title, BodyText: input.BodyText,
	})
	if err != nil {
		return ArticleAssessmentResult{}, err
	}
	evidenceTokens := result.OriginalEvidenceTokens
	truncated := result.EvidenceTruncated
	if evidenceTokens <= 0 {
		evidence, evidenceErr := articlevalue.BuildEvidence(input.Title, input.BodyText)
		if evidenceErr != nil {
			return ArticleAssessmentResult{}, evidenceErr
		}
		evidenceTokens = evidence.OriginalEstimatedTokens
		truncated = evidence.Truncated
	}
	scores := articlevalue.ApplyEvidenceCaps(articlevalue.Scores{
		Quality:   float64(result.QualityScore) / 100,
		Depth:     float64(result.DepthScore) / 100,
		Evergreen: float64(result.EvergreenScore) / 100,
	}, evidenceTokens)
	return ArticleAssessmentResult{
		Quality: scores.Quality, Depth: scores.Depth, Evergreen: scores.Evergreen,
		Confidence: articlevalue.EvidenceConfidence(evidenceTokens, truncated),
		Reasons:    append([]string(nil), result.Reasons...),
		Summary:    strings.TrimSpace(result.Summary),
		Keywords:   append([]string(nil), result.Keywords...),
	}, nil
}

func ConfiguredArticleAssessor() ArticleAssessor {
	if strings.TrimSpace(config.LLMCHATMODEL) == "" {
		return nil
	}
	return EnrichmentArticleAssessor{
		Provider: ConfiguredChatProvider(), Model: config.LLMCHATMODEL,
		Mode: config.ARTICLEASSESSMENTMODE,
	}
}

func ConfiguredChatProvider() ChatProvider {
	return ChatProvider{Client: ConfiguredLLMClient()}
}

func ConfiguredLLMClient() llm.Client {
	timeout, err := time.ParseDuration(strings.TrimSpace(config.LLMTIMEOUT))
	if err != nil || timeout <= 0 {
		timeout = 30 * time.Second
	}
	return llm.Client{
		BaseURL:        config.LLMBASEURL,
		APIKey:         config.LLMAPIKEY,
		ChatModel:      config.LLMCHATMODEL,
		EmbeddingModel: config.LLMEMBEDDINGMODEL,
		Timeout:        timeout,
		CallObserver:   persistLLMCallEvent,
	}
}

var articleAssessmentSlots = struct {
	sync.Mutex
	capacity int
	channel  chan struct{}
}{}

func acquireArticleAssessmentSlot(ctx context.Context) (func(), error) {
	capacity := config.ARTICLEASSESSMENTCONCURRENCY
	if capacity < 1 {
		capacity = 2
	}
	articleAssessmentSlots.Lock()
	if articleAssessmentSlots.channel == nil || articleAssessmentSlots.capacity != capacity {
		articleAssessmentSlots.capacity = capacity
		articleAssessmentSlots.channel = make(chan struct{}, capacity)
	}
	channel := articleAssessmentSlots.channel
	articleAssessmentSlots.Unlock()
	select {
	case channel <- struct{}{}:
		return func() { <-channel }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
