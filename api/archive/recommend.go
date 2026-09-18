package archive

import (
	"DataArk/config"
	"DataArk/llm"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	archiveRecommendPoolLimit = 50
	archiveRecommendCacheTTL  = 10 * time.Minute
)

// ArchiveRecommendDocument 送给模型的归档候选，点击次数只作证据不参与本地打分。
type ArchiveRecommendDocument struct {
	Path       string    `json:"path"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
	Domain     string    `json:"domain"`
	FileName   string    `json:"fileName"`
	SourceURL  string    `json:"sourceUrl"`
	ClickCount int64     `json:"clickCount"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ArchiveRecommendRequest 是一次归档推荐调用。
type ArchiveRecommendRequest struct {
	Window    string
	Limit     int
	Documents []ArchiveRecommendDocument
}

// ArchiveRecommendPick 是模型选出的一篇归档。
type ArchiveRecommendPick struct {
	Path   string  `json:"path"`
	Reason string  `json:"reason"`
	Score  float64 `json:"score"`
}

// ArchiveRecommendResponse 是模型返回的排序结果。
type ArchiveRecommendResponse struct {
	Items []ArchiveRecommendPick `json:"items"`
	Model string                 `json:"model"`
}

// ArchiveRecommender 用 chat 模型从归档池中选出推荐。
type ArchiveRecommender interface {
	RecommendArchives(context.Context, ArchiveRecommendRequest) (ArchiveRecommendResponse, error)
}

type archiveRecommendCacheEntry struct {
	items   []ArchiveRecommendationItem
	expires time.Time
}

var archiveRecommendCache = struct {
	mu      sync.Mutex
	entries map[string]archiveRecommendCacheEntry
}{entries: map[string]archiveRecommendCacheEntry{}}

// configuredArchiveRecommender 可在测试中替换，避免单测打真实 LLM。
var configuredArchiveRecommender = ConfiguredArchiveRecommender

// ArchiveRecommenderFunc 把函数适配成 ArchiveRecommender，方便测试注入。
type ArchiveRecommenderFunc func(context.Context, ArchiveRecommendRequest) (ArchiveRecommendResponse, error)

func (fn ArchiveRecommenderFunc) RecommendArchives(ctx context.Context, request ArchiveRecommendRequest) (ArchiveRecommendResponse, error) {
	return fn(ctx, request)
}

// SwapArchiveRecommenderForTest 替换生产推荐器，返回恢复函数。
func SwapArchiveRecommenderForTest(recommender ArchiveRecommender) func() {
	old := configuredArchiveRecommender
	configuredArchiveRecommender = func() ArchiveRecommender { return recommender }
	return func() { configuredArchiveRecommender = old }
}

// ConfiguredArchiveRecommender 返回生产归档推荐器。
func ConfiguredArchiveRecommender() ArchiveRecommender {
	timeout, err := time.ParseDuration(strings.TrimSpace(config.LLMTIMEOUT))
	if err != nil || timeout <= 0 {
		timeout = 30 * time.Second
	}
	return llmArchiveRecommender{
		client: llm.Client{
			BaseURL:        config.LLMBASEURL,
			APIKey:         config.LLMAPIKEY,
			ChatModel:      config.LLMCHATMODEL,
			EmbeddingModel: config.LLMEMBEDDINGMODEL,
			Timeout:        timeout,
		},
	}
}

type llmArchiveRecommender struct {
	client llm.Client
}

type archiveRecommendModelOutput struct {
	Items []ArchiveRecommendPick `json:"items"`
}

func (recommender llmArchiveRecommender) RecommendArchives(ctx context.Context, request ArchiveRecommendRequest) (ArchiveRecommendResponse, error) {
	if strings.TrimSpace(recommender.client.ChatModel) == "" {
		return ArchiveRecommendResponse{}, errors.New("archive recommender requires a chat model")
	}
	documentsJSON, err := json.Marshal(request.Documents)
	if err != nil {
		return ArchiveRecommendResponse{}, err
	}
	var output archiveRecommendModelOutput
	if _, err := recommender.client.ChatJSON(ctx, []map[string]string{
		{"role": "system", "content": "你是个人知识库的归档推荐编辑。只从给定文档中按 path 选择，不要编造 path。每条必须给出简体中文 reason。点击次数只是参考，不要按公式打分。只返回 JSON。"},
		{"role": "user", "content": fmt.Sprintf("窗口：%s，需要 %d 篇。\n候选（JSON）：\n%s\n返回 JSON：{\"items\":[{\"path\":\"/archive/...\",\"reason\":\"...\",\"score\":0.8}]}", request.Window, request.Limit, string(documentsJSON))},
	}, &output, llm.ChatOptions{
		Stage:          llm.StageArchiveRecommend,
		ResponseFormat: map[string]string{"type": "json_object"},
	}); err != nil {
		return ArchiveRecommendResponse{}, err
	}
	return ArchiveRecommendResponse{
		Items: output.Items,
		Model: strings.TrimSpace(recommender.client.ChatModel),
	}, nil
}

// GetArchiveRecommendations 取最近窗口内归档，强制调用 LLM 选出 limit 条；失败不回退关键词分。
func GetArchiveRecommendations(ctx context.Context, window string, limit int) ([]ArchiveRecommendationItem, error) {
	if db == nil {
		return []ArchiveRecommendationItem{}, nil
	}
	limit = normalizeLimit(limit, 20, 100)
	window = strings.TrimSpace(window)
	if window == "" {
		window = "7d"
	}
	if cached, ok := loadArchiveRecommendCache(window, limit); ok {
		return cached, nil
	}

	query := db.Model(&ArchiveDocument{}).Order("updated_at desc")
	if since, ok := windowStart(window); ok {
		query = query.Where("updated_at >= ?", since)
	}
	var documents []ArchiveDocument
	if err := query.Limit(archiveRecommendPoolLimit).Find(&documents).Error; err != nil {
		return nil, err
	}
	if len(documents) == 0 {
		return []ArchiveRecommendationItem{}, nil
	}

	clicks, err := archiveClickCounts(window)
	if err != nil {
		return nil, err
	}
	pool := make([]ArchiveRecommendDocument, 0, len(documents))
	byPath := make(map[string]ArchiveDocument, len(documents))
	clickByPath := make(map[string]int64, len(documents))
	for _, document := range documents {
		path := archiveRequestPath(document.Domain, document.FileName)
		clickCount := clicks[document.Domain+"/"+document.FileName]
		byPath[path] = document
		clickByPath[path] = clickCount
		pool = append(pool, ArchiveRecommendDocument{
			Path:       path,
			Title:      archiveDisplayTitle(&document),
			Summary:    document.Summary,
			Domain:     document.Domain,
			FileName:   document.FileName,
			SourceURL:  document.SourceURL,
			ClickCount: clickCount,
			UpdatedAt:  document.UpdatedAt,
		})
	}

	recommender := configuredArchiveRecommender()
	if recommender == nil {
		return nil, errors.New("archive recommender is required")
	}
	result, err := recommender.RecommendArchives(ctx, ArchiveRecommendRequest{Window: window, Limit: limit, Documents: pool})
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	recommendations := make([]ArchiveRecommendationItem, 0, limit)
	for _, pick := range result.Items {
		path := strings.TrimSpace(pick.Path)
		reason := strings.TrimSpace(pick.Reason)
		if path == "" || reason == "" {
			continue
		}
		document, ok := byPath[path]
		if !ok {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		recommendations = append(recommendations, ArchiveRecommendationItem{
			Path:       path,
			Domain:     document.Domain,
			FileName:   document.FileName,
			Title:      archiveDisplayTitle(&document),
			Summary:    document.Summary,
			SourceURL:  document.SourceURL,
			Score:      clampArchiveScore(pick.Score),
			UpdatedAt:  document.UpdatedAt,
			Reason:     reason,
			ClickCount: clickByPath[path],
		})
		if len(recommendations) >= limit {
			break
		}
	}
	if len(recommendations) == 0 {
		return nil, errors.New("archive recommender returned no valid items")
	}
	storeArchiveRecommendCache(window, limit, recommendations)
	return recommendations, nil
}

func clampArchiveScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func archiveRecommendCacheKey(window string, limit int) string {
	return fmt.Sprintf("%s:%d", window, limit)
}

func loadArchiveRecommendCache(window string, limit int) ([]ArchiveRecommendationItem, bool) {
	archiveRecommendCache.mu.Lock()
	defer archiveRecommendCache.mu.Unlock()
	entry, ok := archiveRecommendCache.entries[archiveRecommendCacheKey(window, limit)]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	copied := make([]ArchiveRecommendationItem, len(entry.items))
	copy(copied, entry.items)
	return copied, true
}

func storeArchiveRecommendCache(window string, limit int, items []ArchiveRecommendationItem) {
	copied := make([]ArchiveRecommendationItem, len(items))
	copy(copied, items)
	archiveRecommendCache.mu.Lock()
	defer archiveRecommendCache.mu.Unlock()
	archiveRecommendCache.entries[archiveRecommendCacheKey(window, limit)] = archiveRecommendCacheEntry{
		items:   copied,
		expires: time.Now().Add(archiveRecommendCacheTTL),
	}
}

func resetArchiveRecommendCacheForTest() {
	archiveRecommendCache.mu.Lock()
	defer archiveRecommendCache.mu.Unlock()
	archiveRecommendCache.entries = map[string]archiveRecommendCacheEntry{}
}
