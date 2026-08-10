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
	"sync"
	"testing"
)

type fakeOpenAIDoer struct {
	mu        sync.Mutex
	responses []string
	statuses  []int
	paths     []string
	payloads  []map[string]interface{}
	err       error
}

func (fake *fakeOpenAIDoer) Do(req *http.Request) (*http.Response, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
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
	status := http.StatusOK
	if len(fake.statuses) > 0 {
		status = fake.statuses[0]
		fake.statuses = fake.statuses[1:]
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func TestArticleAssessmentStructuredOutputDowngradesOnceAcrossConcurrentCalls(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	valid := `{"choices":[{"message":{"content":"{\"qualityScore\":72,\"depthScore\":64,\"evergreenScore\":81,\"reasons\":[\"specific evidence\",\"limited comparison\"]}"}}]}`
	responses := []string{
		`{"choices":[{"message":{"content":"{\"qualityScore\":72"}}]}`,
		valid, valid, valid, valid, valid,
	}
	client := &fakeOpenAIDoer{responses: responses}
	provider := OpenAICompatibleProvider{BaseURL: "https://fallback.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	var group sync.WaitGroup
	errorsByCall := make(chan error, 5)
	for index := 0; index < 5; index++ {
		group.Add(1)
		go func(candidateID uint) {
			defer group.Done()
			_, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{CandidateID: candidateID, Title: "Title", BodyText: "Body"})
			errorsByCall <- err
		}(uint(index + 1))
	}
	group.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(client.payloads) != 6 {
		t.Fatalf("requests = %d, want one probe plus five compatible calls", len(client.payloads))
	}
	schemaCalls := 0
	for _, payload := range client.payloads {
		format := requireMap(t, payload["response_format"])
		if format["type"] == articleAssessmentResponseSchemaMode {
			schemaCalls++
		}
	}
	if schemaCalls != 1 {
		t.Fatalf("schema calls = %d, payloads=%#v", schemaCalls, client.payloads)
	}
}

func TestArticleAssessmentDoesNotTreatRateLimitAsFormatFailure(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	client := &fakeOpenAIDoer{
		responses: []string{`{"error":{"message":"rate exceeded"}}`},
		statuses:  []int{http.StatusTooManyRequests},
	}
	provider := OpenAICompatibleProvider{BaseURL: "https://rate.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Body"}); err == nil {
		t.Fatal("expected rate-limit error")
	}
	if len(client.payloads) != 1 {
		t.Fatalf("rate-limit requests = %d", len(client.payloads))
	}
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
	resetAssessmentOutputCapabilitiesForTest()
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"qualityScore\":82,\"depthScore\":71,\"evergreenScore\":64,\"reasons\":[\"Evidence is specific\",\"Analysis is concise\"]}"}}]}`,
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "qwen3.5-plus", HTTPClient: client}
	result, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Article body"})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityScore != 82 || result.DepthScore != 71 || result.EvergreenScore != 64 || len(result.Reasons) != 2 {
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
	if schema["additionalProperties"] != false || len(properties) != 4 {
		t.Fatalf("schema = %#v", schema)
	}
	for _, field := range []string{"qualityScore", "depthScore", "evergreenScore", "reasons"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("schema properties = %#v", properties)
		}
	}
}

func TestArticleAssessmentOutputRejectsEverySchemaViolationLocally(t *testing.T) {
	tests := []string{
		`{"depthScore":50,"evergreenScore":50,"reasons":["one","two"]}`,
		`{"qualityScore":50.5,"depthScore":50,"evergreenScore":50,"reasons":["one","two"]}`,
		`{"qualityScore":101,"depthScore":50,"evergreenScore":50,"reasons":["one","two"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["` + strings.Repeat("x", 121) + `","two"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one","two"],"unused":true}`,
	}
	for _, payload := range tests {
		var output articleAssessmentOutput
		decodeErr := decodeChatJSON(payload, &output, true)
		if decodeErr == nil {
			_, decodeErr = output.result()
		}
		if decodeErr == nil {
			t.Fatalf("invalid output accepted: %s", payload)
		}
	}
}

func TestOpenAICompatibleProviderLogsUsageWithoutPayloadText(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	logOutput := captureStandardLog(t)
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"qualityScore\":80,\"depthScore\":70,\"evergreenScore\":60,\"reasons\":[\"completion-text-sentinel\",\"No major limitation\"]}","reasoning_content":"reasoning-text-sentinel"}}],"usage":{"prompt_tokens":101,"completion_tokens":20,"total_tokens":121,"prompt_tokens_details":{"cached_tokens":11},"completion_tokens_details":{"reasoning_tokens":0}}}`,
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "deepseek-v4", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "prompt-text-sentinel"}); err != nil {
		t.Fatal(err)
	}
	event := decodeSingleLLMEvent(t, logOutput.String())
	if event["status"] != "success" || event["llm_stage"] != llmStageArticleAssessment || event["llm_model"] != "deepseek-v4" {
		t.Fatalf("event identity = %#v", event)
	}
	if event["llm_response_mode"] != articleAssessmentResponseSchemaMode || event["llm_attempt"] != float64(1) || event["llm_evidence_tokens"] == nil || event["llm_original_evidence_tokens"] == nil {
		t.Fatalf("assessment call metadata = %#v", event)
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
	resetAssessmentOutputCapabilitiesForTest()
	logOutput := captureStandardLog(t)
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"qualityScore\":80,\"depthScore\":70,\"evergreenScore\":60,\"reasons\":[\"ok\",\"limit\"],\"unused\":\"must fail\"}"}}],"usage":{"prompt_tokens":80,"completion_tokens":16,"total_tokens":96,"completion_tokens_details":{"reasoning_tokens":3}}}`,
		`{"choices":[{"message":{"content":"{\"qualityScore\":80,\"depthScore\":70,\"evergreenScore\":60,\"reasons\":[\"ok\",\"limit\"],\"unused\":\"must fail\"}"}}],"usage":{"prompt_tokens":81,"completion_tokens":17,"total_tokens":98}}`,
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Body"}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
	events := decodeLLMEvents(t, logOutput.String())
	if len(events) != 2 || events[0]["status"] != "failed" || events[1]["status"] != "failed" || events[1]["error_type"] != "invalid_output" {
		t.Fatalf("events = %#v", events)
	}
	usage := requireMap(t, events[0]["llm_usage"])
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

func decodeLLMEvents(t *testing.T, output string) []map[string]interface{} {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	events := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		payload := strings.TrimPrefix(strings.TrimSpace(line), "dataark_event ")
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatalf("decode event: %v; line=%q", err, line)
		}
		events = append(events, event)
	}
	return events
}
