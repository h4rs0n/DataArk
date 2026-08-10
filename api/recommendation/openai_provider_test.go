package recommendation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
)

type fakeOpenAIDoer struct {
	responses []string
	paths     []string
	payloads  []map[string]interface{}
	err       error
}

func (fake *fakeOpenAIDoer) Do(req *http.Request) (*http.Response, error) {
	fake.paths = append(fake.paths, req.URL.Path)
	requestBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(requestBody, &payload); err != nil {
		return nil, err
	}
	fake.payloads = append(fake.payloads, payload)
	if fake.err != nil {
		return nil, fake.err
	}
	body := `{}`
	if len(fake.responses) > 0 {
		body = fake.responses[0]
		fake.responses = fake.responses[1:]
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func TestOpenAICompatibleProviderEmbed(t *testing.T) {
	client := &fakeOpenAIDoer{responses: []string{`{"data":[{"embedding":[0.1,0.2]},{"embedding":[0.3,0.4]}]}`}}
	provider := OpenAICompatibleProvider{
		BaseURL:        "https://llm.example/v1",
		APIKey:         "test-key",
		EmbeddingModel: "text-embedding",
		HTTPClient:     client,
	}
	vectors, err := provider.Embed(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || len(vectors[0]) != 2 || vectors[1][1] != float32(0.4) {
		t.Fatalf("vectors = %#v", vectors)
	}
	if len(client.paths) != 1 || client.paths[0] != "/v1/embeddings" {
		t.Fatalf("paths = %#v", client.paths)
	}
}

func TestOpenAICompatibleProviderEnrichAndRerank(t *testing.T) {
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"summary\":\"Short\",\"topics\":[\"Go\"],\"entities\":[\"DataArk\"],\"contentType\":\"article\",\"contentStyle\":\"technical\",\"language\":\"en\",\"qualityScore\":0.8,\"depthScore\":0.7}"}}]}`,
		`{"choices":[{"message":{"content":"{\"items\":[{\"candidateId\":2,\"rank\":1,\"reason\":\"Better fit\",\"confidence\":0.9}]}"}}]}`,
	}}
	provider := OpenAICompatibleProvider{
		BaseURL:    "https://llm.example",
		ChatModel:  "MiMo-V2.5-Pro",
		HTTPClient: client,
	}
	enriched, err := provider.Enrich(context.Background(), EnrichmentInput{CandidateID: 3, Title: "Go article"})
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Summary != "Short" || enriched.Topics[0] != "Go" || enriched.Model != "MiMo-V2.5-Pro" {
		t.Fatalf("enriched = %#v", enriched)
	}
	reranked, err := provider.Rerank(context.Background(), RerankInput{
		UserID:         1,
		RequestedCount: 1,
		Candidates: []RerankCandidate{
			{CandidateID: 2, Title: "Second"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reranked.Items) != 1 || reranked.Items[0].CandidateID != 2 || reranked.Model != "MiMo-V2.5-Pro" {
		t.Fatalf("reranked = %#v", reranked)
	}
	if len(client.paths) != 2 || client.paths[0] != "/v1/chat/completions" || client.paths[1] != "/v1/chat/completions" {
		t.Fatalf("paths = %#v", client.paths)
	}
	for index, payload := range client.payloads {
		if payload["max_tokens"] != float64(llmChatMaxTokens) {
			t.Fatalf("payload %d max_tokens = %#v", index, payload["max_tokens"])
		}
		thinking, ok := payload["thinking"].(map[string]interface{})
		if !ok || thinking["type"] != "disabled" {
			t.Fatalf("payload %d thinking = %#v", index, payload["thinking"])
		}
	}
}

func TestOpenAICompatibleProviderArticleAssessmentUsesCompactSchemaAndQwenSwitch(t *testing.T) {
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"qualityScore\":0.82,\"depthScore\":0.71,\"reasons\":[\"Evidence is specific\",\"Analysis is concise\"]}"}}]}`,
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "qwen3.5-plus", HTTPClient: client}
	result, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Article body"})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityScore != 0.82 || result.DepthScore != 0.71 || len(result.Reasons) != 2 {
		t.Fatalf("result = %#v", result)
	}
	payload := client.payloads[0]
	if payload["max_tokens"] != float64(llmChatMaxTokens) || payload["enable_thinking"] != false {
		t.Fatalf("request controls = %#v", payload)
	}
	responseFormat := requireMap(t, payload["response_format"])
	if responseFormat["type"] != "json_schema" {
		t.Fatalf("response format = %#v", responseFormat)
	}
	jsonSchema := requireMap(t, responseFormat["json_schema"])
	if jsonSchema["strict"] != true {
		t.Fatalf("json schema = %#v", jsonSchema)
	}
	schema := requireMap(t, jsonSchema["schema"])
	properties := requireMap(t, schema["properties"])
	if schema["additionalProperties"] != false || len(properties) != 3 {
		t.Fatalf("schema = %#v", schema)
	}
	for _, field := range []string{"qualityScore", "depthScore", "reasons"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("schema properties = %#v", properties)
		}
	}
}

func TestOpenAICompatibleProviderLogsUsageWithoutPayloadText(t *testing.T) {
	logOutput := captureStandardLog(t)
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"qualityScore\":0.8,\"depthScore\":0.7,\"reasons\":[\"completion-text-sentinel\"]}","reasoning_content":"reasoning-text-sentinel"}}],"usage":{"prompt_tokens":101,"completion_tokens":20,"total_tokens":121,"prompt_tokens_details":{"cached_tokens":11},"completion_tokens_details":{"reasoning_tokens":0}}}`,
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "deepseek-v4", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "prompt-text-sentinel"}); err != nil {
		t.Fatal(err)
	}
	event := decodeSingleLLMEvent(t, logOutput.String())
	if event["status"] != "success" || event["llm_stage"] != llmStageArticleAssessment || event["llm_model"] != "deepseek-v4" {
		t.Fatalf("event identity = %#v", event)
	}
	if duration, ok := event["duration_ms"].(float64); !ok || duration < 1 {
		t.Fatalf("duration_ms = %#v", event["duration_ms"])
	}
	usage := requireMap(t, event["llm_usage"])
	want := map[string]interface{}{
		"available": true, "prompt_tokens": float64(101), "completion_tokens": float64(20),
		"reasoning_tokens": float64(0), "cached_tokens": float64(11), "total_tokens": float64(121),
	}
	for field, expected := range want {
		if usage[field] != expected {
			t.Fatalf("usage[%s] = %#v, want %#v; usage=%#v", field, usage[field], expected, usage)
		}
	}
	for _, forbidden := range []string{"prompt-text-sentinel", "completion-text-sentinel", "reasoning-text-sentinel"} {
		if strings.Contains(logOutput.String(), forbidden) {
			t.Fatalf("payload text %q leaked in %q", forbidden, logOutput.String())
		}
	}
}

func TestOpenAICompatibleProviderLogsUsageWhenStrictAssessmentOutputIsInvalid(t *testing.T) {
	logOutput := captureStandardLog(t)
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"qualityScore\":0.8,\"depthScore\":0.7,\"reasons\":[\"ok\"],\"unused\":\"must fail\"}"}}],"usage":{"prompt_tokens":80,"completion_tokens":16,"total_tokens":96,"completion_tokens_details":{"reasoning_tokens":3}}}`,
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Body"}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
	event := decodeSingleLLMEvent(t, logOutput.String())
	if event["status"] != "failed" || event["error_type"] != "invalid_output" {
		t.Fatalf("event = %#v", event)
	}
	usage := requireMap(t, event["llm_usage"])
	if usage["prompt_tokens"] != float64(80) || usage["reasoning_tokens"] != float64(3) {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestOpenAICompatibleProviderLogsUnavailableUsageOnRequestFailure(t *testing.T) {
	logOutput := captureStandardLog(t)
	client := &fakeOpenAIDoer{err: errors.New("upstream unavailable")}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.Rerank(context.Background(), RerankInput{UserID: 9, RequestedCount: 1}); err == nil {
		t.Fatal("expected rerank error")
	}
	event := decodeSingleLLMEvent(t, logOutput.String())
	if event["status"] != "failed" || event["error_type"] != "provider_request" || event["llm_stage"] != llmStageRecommendationRerank {
		t.Fatalf("event = %#v", event)
	}
	usage := requireMap(t, event["llm_usage"])
	if usage["available"] != false || usage["prompt_tokens"] != float64(0) || usage["total_tokens"] != float64(0) {
		t.Fatalf("usage = %#v", usage)
	}
}

func requireMap(t *testing.T, value interface{}) map[string]interface{} {
	t.Helper()
	result, ok := value.(map[string]interface{})
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return result
}

func captureStandardLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})
	return &output
}

func decodeSingleLLMEvent(t *testing.T, output string) map[string]interface{} {
	t.Helper()
	if count := strings.Count(output, "dataark_event "); count != 1 {
		t.Fatalf("event count = %d; output = %q", count, output)
	}
	line := strings.TrimSpace(output)
	payload := strings.TrimPrefix(line, "dataark_event ")
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("decode event: %v; output = %q", err, output)
	}
	if event["event"] != llmCallEventName {
		t.Fatalf("event = %#v", event)
	}
	return event
}
