// 共享 OpenAI 兼容 HTTP 客户端：评估与推荐都走同一套 chat / embedding，避免两套 token 日志漂移。
package llm

import (
	"DataArk/observability"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	ChatMaxTokens         = 32768
	ChatRerankMaxTokens   = 2048 // 只给日报重排留补全预算，避免 max_tokens 占满整段上下文。
	ChatTemperature       = 0.7
	ChatTopP              = 0.80
	ChatTopK              = 20
	ChatMinP              = 0.0
	ChatPresencePenalty   = 1.5
	ChatRepetitionPenalty = 1.0

	// 各调用阶段名写入 llm_call 日志，供评估面板按 stage 聚合。
	StageArticleAssessment    = "article_assessment"
	StageRecommendationRerank = "recommendation_rerank"
	StageDigestSummary        = "digest_summary"
	CallEventName             = "llm_call"
)

// HTTPDoer 方便测试替换 DefaultClient。
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client 是无状态的 OpenAI 兼容网关封装；禁止在日志里写入 prompt 或模型正文。
type Client struct {
	BaseURL        string
	APIKey         string
	ChatModel      string
	EmbeddingModel string
	Timeout        time.Duration
	HTTPClient     HTTPDoer
	CallObserver   func(observability.Event)
}

// ChatOptions 只携带安全的观测字段，不含消息文本。
type ChatOptions struct {
	Stage                  string
	CandidateID            uint
	UserID                 uint
	ResponseFormat         interface{}
	ResponseMode           string
	Attempt                int
	EvidenceTokens         int
	OriginalEvidenceTokens int
	EvidenceTruncated      bool
	StrictOutput           bool
	ValidateOutput         func() error
	MaxTokens              int // 覆盖默认补全上限；0 表示使用 ChatMaxTokens。
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		PromptDetails    struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
	// Timings 是 llama.cpp 的 decode/prefill 计时；其他 OpenAI 兼容后端通常省略。
	Timings *struct {
		PredictedN  int     `json:"predicted_n"`
		PredictedMS float64 `json:"predicted_ms"`
	} `json:"timings"`
}

func (client Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if strings.TrimSpace(client.EmbeddingModel) == "" {
		return nil, errors.New("missing embedding model")
	}
	var response struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := client.PostJSON(ctx, "/embeddings", map[string]interface{}{
		"model": strings.TrimSpace(client.EmbeddingModel),
		"input": texts,
	}, &response); err != nil {
		return nil, err
	}
	vectors := make([][]float32, 0, len(response.Data))
	for _, item := range response.Data {
		vector := make([]float32, 0, len(item.Embedding))
		for _, value := range item.Embedding {
			vector = append(vector, float32(value))
		}
		vectors = append(vectors, vector)
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: got %d want %d", len(vectors), len(texts))
	}
	return vectors, nil
}

// chatCompletionMaxTokens 允许 rerank 等短输出阶段覆盖默认 32k 补全上限。
func chatCompletionMaxTokens(options ChatOptions) int {
	if options.MaxTokens > 0 {
		return options.MaxTokens
	}
	return ChatMaxTokens
}

// ChatJSON 调用 /chat/completions 并把 content 解成 JSON。失败时仍写 llm_call。
func (client Client) ChatJSON(ctx context.Context, messages []map[string]string, output interface{}, options ChatOptions) (content string, callErr error) {
	startedAt := time.Now()
	model := strings.TrimSpace(client.ChatModel)
	var response chatCompletionResponse
	defer func() {
		client.logChatCall(options, model, &response, time.Since(startedAt), callErr)
	}()
	if model == "" {
		return "", errors.New("missing chat model")
	}
	payload := map[string]interface{}{
		"model":              model,
		"messages":           messages,
		"temperature":        ChatTemperature,
		"max_tokens":         chatCompletionMaxTokens(options),
		"top_p":              ChatTopP,
		"top_k":              ChatTopK,
		"min_p":              ChatMinP,
		"presence_penalty":   ChatPresencePenalty,
		"repetition_penalty": ChatRepetitionPenalty,
	}
	if options.ResponseFormat != nil {
		payload["response_format"] = options.ResponseFormat
	}
	disableModelThinking(payload, model)
	if err := client.PostJSON(ctx, "/chat/completions", payload, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 {
		return "", errors.New("empty chat completion response")
	}
	content = strings.TrimSpace(response.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("empty chat completion response")
	}
	if err := DecodeChatJSON(content, output, options.StrictOutput); err != nil {
		return content, fmt.Errorf("invalid chat JSON: %w", err)
	}
	if options.ValidateOutput != nil {
		if err := options.ValidateOutput(); err != nil {
			return content, fmt.Errorf("invalid chat JSON: %w", err)
		}
	}
	return content, nil
}

// DecodeChatJSON 把模型 content 解成结构体；strict 时拒绝未知字段和尾随 JSON。
func DecodeChatJSON(content string, output interface{}, strict bool) error {
	if !strict {
		return json.Unmarshal([]byte(content), output)
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

// disableModelThinking 按模型族关闭推理。Qwen 走 vLLM/sglang 的 chat_template_kwargs，
// 仅顶层 enable_thinking=false 会被忽略并继续输出 reasoning_content。
func disableModelThinking(payload map[string]interface{}, model string) {
	model = strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(model, "qwen") {
		payload["enable_thinking"] = false
		payload["chat_template_kwargs"] = map[string]interface{}{"enable_thinking": false}
		return
	}
	for _, marker := range []string{"deepseek", "mimo", "kimi", "minimax", "glm", "hy3", "hunyuan"} {
		if strings.Contains(model, marker) {
			payload["thinking"] = map[string]string{"type": "disabled"}
			return
		}
	}
}

func (client Client) logChatCall(options ChatOptions, model string, response *chatCompletionResponse, elapsed time.Duration, callErr error) {
	durationMilliseconds := elapsed.Milliseconds()
	if durationMilliseconds <= 0 {
		durationMilliseconds = 1
	}
	usage := &observability.LLMUsage{}
	if response != nil && response.Usage != nil {
		usage.Available = true
		usage.PromptTokens = response.Usage.PromptTokens
		usage.CompletionTokens = response.Usage.CompletionTokens
		usage.ReasoningTokens = response.Usage.CompletionDetails.ReasoningTokens
		usage.CachedTokens = response.Usage.PromptDetails.CachedTokens
		usage.TotalTokens = response.Usage.TotalTokens
	}
	if response != nil && response.Timings != nil {
		usage.PredictedTokens = response.Timings.PredictedN
		usage.PredictedMS = decodeDurationMS(response.Timings.PredictedMS)
		if usage.PredictedTokens > 0 || usage.PredictedMS > 0 {
			usage.Available = true
		}
	}
	event := observability.Event{
		Name: CallEventName, Status: "success", CandidateID: options.CandidateID, UserID: options.UserID,
		LLMStage: options.Stage, LLMModel: model, LLMResponseMode: options.ResponseMode, LLMAttempt: options.Attempt,
		LLMEvidenceTokens: options.EvidenceTokens, LLMOriginalEvidenceTokens: options.OriginalEvidenceTokens,
		LLMEvidenceTruncated: options.EvidenceTruncated, LLMDuration: durationMilliseconds, LLMUsage: usage,
	}
	if callErr != nil {
		event.Status = "failed"
		event.ErrorType = ClassifyCallError(callErr)
		event.ErrorMessage = callErr.Error()
	}
	observability.Log(event)
	if client.CallObserver != nil {
		client.CallObserver(event)
	}
}

// decodeDurationMS 把 llama.cpp timings.predicted_ms 收成毫秒整数；正值不足 1ms 记 1。
func decodeDurationMS(predictedMS float64) int64 {
	if predictedMS <= 0 {
		return 0
	}
	ms := int64(math.Round(predictedMS))
	if ms < 1 {
		return 1
	}
	return ms
}

// ClassifyCallError 把网关错误压成固定短码，供日志和评估指标共用。
func ClassifyCallError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "invalid chat json"):
		return "invalid_output"
	case strings.Contains(message, "empty chat completion"):
		return "empty_output"
	case strings.Contains(message, "status 429"):
		return "rate_limit"
	case strings.Contains(message, "status 401"), strings.Contains(message, "status 402"), strings.Contains(message, "status 403"):
		return "provider_access"
	case strings.Contains(message, "status "):
		return "provider_http"
	default:
		return "provider_request"
	}
}

// PostJSON 向 /v1 路径发 JSON；响应体上限 4MiB。
func (client Client) PostJSON(ctx context.Context, path string, payload interface{}, output interface{}) error {
	baseURL := strings.TrimRight(strings.TrimSpace(client.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	body, _ := json.Marshal(payload)
	requestCtx := ctx
	cancel := func() {}
	if client.Timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, client.Timeout)
	}
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(client.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(client.APIKey))
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("llm request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if err := json.Unmarshal(responseBody, output); err != nil {
		return fmt.Errorf("invalid llm response JSON: %w", err)
	}
	return nil
}
