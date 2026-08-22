package recommendation

import (
	"DataArk/config"
	"DataArk/discovery"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// enrichment.go 配置富化/rerank 提供者，并富化发现候选。

// ConfiguredEnrichmentProvider 返回生产富化提供者；未配置 LLM 时用规则实现。
func ConfiguredEnrichmentProvider() EnrichmentProvider {
	if strings.TrimSpace(config.LLMCHATMODEL) == "" {
		return RuleBasedEnrichmentProvider{}
	}
	return configuredOpenAICompatibleProvider()
}

// ConfiguredRecommendationReranker 返回生产 reranker；未配置 LLM 时返回 nil。
func ConfiguredRecommendationReranker() RerankProvider {
	if strings.TrimSpace(config.LLMCHATMODEL) == "" {
		return nil
	}
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

// ConfiguredOpenAICompatibleProvider 返回生产评估用的 OpenAI 兼容提供者，不通过 HTTP 暴露凭证。
func ConfiguredOpenAICompatibleProvider() OpenAICompatibleProvider {
	return configuredOpenAICompatibleProvider()
}

// EnrichDiscoveryCandidate 用指定提供者富化一条发现候选。
func EnrichDiscoveryCandidate(ctx context.Context, candidateID uint, provider EnrichmentProvider) (*DiscoveryCandidate, error) {
	if provider == nil {
		return nil, errors.New("missing enrichment provider")
	}
	if db == nil || candidateID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return nil, err
	}
	input := EnrichmentInput{
		CandidateID: candidate.ID,
		URL:         candidate.URL,
		Title:       candidate.Title,
		Summary:     candidate.Summary,
		BodyText:    candidate.BodyText,
		PublishedAt: candidate.PublishedAt,
	}
	result, err := provider.Enrich(ctx, input)
	if err != nil {
		_ = db.Model(&candidate).Updates(map[string]interface{}{
			"enrichment_status": RecommendationEnrichmentStatusFailed,
			"enrichment_error":  err.Error(),
			"updated_at":        time.Now(),
		}).Error
		return nil, err
	}

	bodyForHash := candidate.BodyText
	if strings.TrimSpace(bodyForHash) == "" {
		bodyForHash = candidate.Summary
	}
	contentHash := candidate.ContentHash
	if strings.TrimSpace(bodyForHash) != "" {
		contentHash = discovery.ContentHash(bodyForHash)
	}
	normalizedURL := candidate.NormalizedURL
	if normalizedURL == "" {
		if value, err := discovery.NormalizeArticleURL(candidate.URL); err == nil {
			normalizedURL = value
		}
	}
	canonicalURL := candidate.CanonicalURL
	if canonicalURL == "" {
		canonicalURL = normalizedURL
	}
	dedupeKey := normalizedURL
	if contentHash != "" {
		dedupeKey = contentHash
	}
	topics, _ := json.Marshal(result.Topics)
	entities, _ := json.Marshal(result.Entities)

	updates := map[string]interface{}{
		"summary":           firstNonEmpty(result.Summary, candidate.Summary),
		"normalized_url":    normalizedURL,
		"canonical_url":     canonicalURL,
		"content_hash":      contentHash,
		"dedupe_key":        dedupeKey,
		"topics":            string(topics),
		"entities":          string(entities),
		"content_type":      strings.TrimSpace(result.ContentType),
		"content_style":     strings.TrimSpace(result.ContentStyle),
		"language":          strings.TrimSpace(result.Language),
		"llm_model":         strings.TrimSpace(result.Model),
		"prompt_version":    strings.TrimSpace(result.PromptVersion),
		"enrichment_status": RecommendationEnrichmentStatusReady,
		"enrichment_error":  "",
		"enriched_at":       time.Now(),
		"updated_at":        time.Now(),
	}
	if err := db.Model(&candidate).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return nil, err
	}
	return &candidate, nil
}
