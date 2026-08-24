package recommendation

import (
	"DataArk/assessment"
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

func TestArticleAssessmentUnsupportedSchemaCachesJSONObjectForLaterCalls(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	valid := chatCompletionWithContent(articleAssessmentJSON(72, 64, 81, "specific evidence", "limited comparison"))
	client := &fakeOpenAIDoer{
		responses: []string{`{"error":{"message":"response_format json_schema is not supported"}}`, valid, valid, valid, valid, valid, valid},
		statuses:  []int{http.StatusBadRequest, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK},
	}
	provider := OpenAICompatibleProvider{BaseURL: "https://fallback.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{CandidateID: 1, Title: "Title", BodyText: "Body"}); err != nil {
		t.Fatal(err)
	}
	if len(client.payloads) != 2 {
		t.Fatalf("probe requests = %d, want schema rejection plus one json_object", len(client.payloads))
	}
	if requireMap(t, client.payloads[0]["response_format"])["type"] != articleAssessmentResponseSchemaMode {
		t.Fatalf("first format = %#v", client.payloads[0]["response_format"])
	}
	if requireMap(t, client.payloads[1]["response_format"])["type"] != articleAssessmentResponseObjectMode {
		t.Fatalf("second format = %#v", client.payloads[1]["response_format"])
	}

	var group sync.WaitGroup
	errorsByCall := make(chan error, 5)
	for index := 0; index < 5; index++ {
		group.Add(1)
		go func(candidateID uint) {
			defer group.Done()
			_, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{CandidateID: candidateID, Title: "Title", BodyText: "Body"})
			errorsByCall <- err
		}(uint(index + 2))
	}
	group.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(client.payloads) != 7 {
		t.Fatalf("requests = %d, want one probe plus six json_object calls", len(client.payloads))
	}
	schemaCalls := 0
	for _, payload := range client.payloads {
		assertChatSampling(t, payload)
		if requireMap(t, payload["response_format"])["type"] == articleAssessmentResponseSchemaMode {
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

func TestOpenAICompatibleProviderRerank(t *testing.T) {
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"items\":[{\"candidateId\":2,\"rank\":1,\"reason\":\"Better fit\",\"confidence\":0.9}]}"}}]}`,
	}}
	provider := OpenAICompatibleProvider{
		BaseURL:    "https://llm.example",
		ChatModel:  "MiMo-V2.5-Pro",
		HTTPClient: client,
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
	if len(client.paths) != 1 || client.paths[0] != "/v1/chat/completions" {
		t.Fatalf("paths = %#v", client.paths)
	}
	assertChatSamplingWithMaxTokens(t, client.payloads[0], llmChatRerankMaxTokens)
	thinking, ok := client.payloads[0]["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %#v", client.payloads[0]["thinking"])
	}
}

func TestOpenAICompatibleProviderGenerateDigestSummary(t *testing.T) {
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"overview\":\"今日两篇技术文章。\",\"highlights\":[\"Go 并发实践\"],\"topics\":[\"go\",\"systems\"]}"}}]}`,
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	output, err := provider.GenerateDigestSummary(context.Background(), DigestSummaryInput{
		Date: "2026-06-01",
		Items: []DigestSummaryItem{
			{Rank: 1, Title: "Go concurrency", Summary: "About goroutines", Source: "go.example", Topics: []string{"go"}, Reason: "matches profile"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Overview != "今日两篇技术文章。" || len(output.Highlights) != 1 || len(output.Topics) != 2 {
		t.Fatalf("output = %#v", output)
	}
	if output.Model != "MiMo-V2.5-Pro" || output.PromptVersion != "openai-compatible-digest-summary-v1" {
		t.Fatalf("identity = %#v", output)
	}
	payload := client.payloads[0]
	assertChatSampling(t, payload)
	responseFormat := requireMap(t, payload["response_format"])
	if responseFormat["type"] != "json_object" {
		t.Fatalf("response format = %#v", responseFormat)
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", payload["messages"])
	}
	userMessage := requireMap(t, messages[1])
	content, _ := userMessage["content"].(string)
	if !strings.Contains(content, "2026-06-01") || !strings.Contains(content, "Go concurrency") {
		t.Fatalf("user message = %q", content)
	}
}

func TestOpenAICompatibleProviderGenerateDigestSummaryRejectsInvalidOutput(t *testing.T) {
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":"{\"overview\":\"\",\"highlights\":[]}"}}]}`,
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.GenerateDigestSummary(context.Background(), DigestSummaryInput{Date: "2026-06-01"}); err == nil {
		t.Fatal("expected validation error for empty overview")
	}
}

func TestOpenAICompatibleProviderArticleAssessmentUsesCompactSchemaAndQwenSwitch(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	client := &fakeOpenAIDoer{responses: []string{
		chatCompletionWithContent(articleAssessmentJSON(82, 71, 64, "Evidence is specific", "Analysis is concise")),
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "qwen3.5-plus", HTTPClient: client}
	result, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Article body"})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityScore != 82 || result.DepthScore != 71 || result.EvergreenScore != 64 || len(result.Reasons) != 2 || result.Summary == "" || len(result.Keywords) != 3 {
		t.Fatalf("result = %#v", result)
	}
	payload := client.payloads[0]
	assertChatSampling(t, payload)
	if payload["enable_thinking"] != false {
		t.Fatalf("request controls = %#v", payload)
	}
	templateKwargs, ok := payload["chat_template_kwargs"].(map[string]interface{})
	if !ok || templateKwargs["enable_thinking"] != false {
		t.Fatalf("chat_template_kwargs = %#v", payload["chat_template_kwargs"])
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
	if schema["additionalProperties"] != false || len(properties) != 6 {
		t.Fatalf("schema = %#v", schema)
	}
	for _, field := range []string{"qualityScore", "depthScore", "evergreenScore", "reasons", "summary", "keywords"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("schema properties = %#v", properties)
		}
	}
	messages := payload["messages"].([]interface{})
	user := requireMap(t, messages[1])
	prompt, _ := user["content"].(string)
	if !strings.Contains(prompt, "summary：") || !strings.Contains(prompt, "keywords：") || !strings.Contains(prompt, "简体中文") || !strings.Contains(prompt, "无论原文语种如何") {
		t.Fatalf("prompt missing Chinese output contract: %#v", user["content"])
	}
	system := requireMap(t, messages[0])
	systemContent, _ := system["content"].(string)
	if !strings.Contains(systemContent, "你是文章阅读价值评估器") || !strings.Contains(systemContent, "简体中文") {
		t.Fatalf("system prompt missing Chinese contract: %#v", system["content"])
	}
}

func TestArticleAssessmentOutputRejectsEverySchemaViolationLocally(t *testing.T) {
	tests := []string{
		`{"depthScore":50,"evergreenScore":50,"reasons":["one","two"]}`,
		`{"qualityScore":50.5,"depthScore":50,"evergreenScore":50,"reasons":["one","two"]}`,
		`{"qualityScore":101,"depthScore":50,"evergreenScore":50,"reasons":["one","two"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["` + strings.Repeat("x", 121) + `","two"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one","two"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one","two"],"summary":"ok","keywords":["a","b"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one","two"],"summary":"` + strings.Repeat("x", 201) + `","keywords":["a","b","c"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one","two"],"summary":"ok","keywords":["` + strings.Repeat("x", 21) + `","b","c"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one","two"],"summary":"ok","keywords":["a","b","c","d","e","f","g","h","i"]}`,
		`{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one","two"],"summary":"ok","keywords":["a","b","c"],"unused":true}`,
	}
	for _, payload := range tests {
		if err := assessment.ValidateChatAssessmentJSON(payload); err == nil {
			t.Fatalf("invalid output accepted: %s", payload)
		}
	}
}

func TestOpenAICompatibleProviderLogsUsageWithoutPayloadText(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	logOutput := captureStandardLog(t)
	client := &fakeOpenAIDoer{responses: []string{
		`{"choices":[{"message":{"content":` + mustJSONString(articleAssessmentJSON(80, 70, 60, "completion-text-sentinel", "No major limitation")) + `,"reasoning_content":"reasoning-text-sentinel"}}],"usage":{"prompt_tokens":101,"completion_tokens":20,"total_tokens":121,"prompt_tokens_details":{"cached_tokens":11},"completion_tokens_details":{"reasoning_tokens":0}}}`,
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
	invalid := `{"choices":[{"message":{"content":"{\"qualityScore\":80,\"depthScore\":70,\"evergreenScore\":60,\"reasons\":[\"ok\",\"limit\"],\"summary\":\"ok\",\"keywords\":[\"a\",\"b\",\"c\"],\"unused\":\"must fail\"}"}}],"usage":{"prompt_tokens":80,"completion_tokens":16,"total_tokens":96,"completion_tokens_details":{"reasoning_tokens":3}}}`
	responses := make([]string, articleAssessmentSchemaMaxAttempts+1)
	for index := range responses {
		responses[index] = invalid
	}
	client := &fakeOpenAIDoer{responses: responses}
	provider := OpenAICompatibleProvider{BaseURL: "https://llm.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Body"}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
	events := decodeLLMEvents(t, logOutput.String())
	if len(events) != articleAssessmentSchemaMaxAttempts+1 {
		t.Fatalf("event count = %d, want %d; events=%#v", len(events), articleAssessmentSchemaMaxAttempts+1, events)
	}
	for index, event := range events {
		if event["status"] != "failed" || event["error_type"] != "invalid_output" {
			t.Fatalf("event %d = %#v", index, event)
		}
		if event["llm_attempt"] != float64(index+1) {
			t.Fatalf("event %d llm_attempt = %#v", index, event["llm_attempt"])
		}
		message, _ := event["error_message"].(string)
		if !strings.Contains(message, "unknown field") {
			t.Fatalf("event %d error_message = %q", index, message)
		}
		wantMode := articleAssessmentResponseSchemaMode
		if index == articleAssessmentSchemaMaxAttempts {
			wantMode = articleAssessmentResponseObjectMode
		}
		if event["llm_response_mode"] != wantMode {
			t.Fatalf("event %d llm_response_mode = %#v", index, event["llm_response_mode"])
		}
	}
	usage := requireMap(t, events[0]["llm_usage"])
	if usage["prompt_tokens"] != float64(80) || usage["reasoning_tokens"] != float64(3) {
		t.Fatalf("usage = %#v", usage)
	}
	if strings.Contains(logOutput.String(), "must fail") {
		t.Fatalf("completion text leaked in %q", logOutput.String())
	}
}

func TestArticleAssessmentRetriesSchemaWithConcreteValidatorError(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	invalid := `{"qualityScore":80,"depthScore":70,"evergreenScore":60,"reasons":["ok","limit"],"summary":"ok","keywords":["a","b","c"],"unused":true}`
	client := &fakeOpenAIDoer{responses: []string{
		chatCompletionWithContent(invalid),
		chatCompletionWithContent(articleAssessmentJSON(80, 70, 60, "ok", "limit")),
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://retry.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	result, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	if result.QualityScore != 80 || len(client.payloads) != 2 {
		t.Fatalf("result=%#v requests=%d", result, len(client.payloads))
	}
	if requireMap(t, client.payloads[0]["response_format"])["type"] != articleAssessmentResponseSchemaMode {
		t.Fatalf("first format = %#v", client.payloads[0]["response_format"])
	}
	if requireMap(t, client.payloads[1]["response_format"])["type"] != articleAssessmentResponseSchemaMode {
		t.Fatalf("retry format = %#v", client.payloads[1]["response_format"])
	}
	assertChatSampling(t, client.payloads[1])
	messages := payloadMessages(t, client.payloads[1])
	if len(messages) != 3 {
		t.Fatalf("retry messages = %#v", messages)
	}
	assertNoAssistantReplay(t, client.payloads[1], invalid)
	user := requireMap(t, messages[len(messages)-1])
	content, _ := user["content"].(string)
	if user["role"] != "user" || !strings.Contains(content, `unknown field "unused"`) || !strings.Contains(content, "解析/校验错误：") || !strings.Contains(content, "简体中文") {
		t.Fatalf("retry user = %q", content)
	}
	if strings.Contains(content, "invalid chat json") {
		t.Fatalf("retry user used classification wrapper: %q", content)
	}
}

func TestArticleAssessmentFallsBackToJSONObjectAfterFiveSchemaRetriesWithoutCaching(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	invalid := chatCompletionWithContent(`{"qualityScore":80,"depthScore":70,"evergreenScore":60,"reasons":["ok","limit"],"summary":"ok","keywords":["a","b","c"],"unused":true}`)
	valid := chatCompletionWithContent(articleAssessmentJSON(72, 64, 81, "specific evidence", "limited comparison"))
	responses := make([]string, articleAssessmentSchemaMaxAttempts+2)
	for index := 0; index < articleAssessmentSchemaMaxAttempts; index++ {
		responses[index] = invalid
	}
	responses[articleAssessmentSchemaMaxAttempts] = valid
	responses[articleAssessmentSchemaMaxAttempts+1] = valid
	client := &fakeOpenAIDoer{responses: responses}
	provider := OpenAICompatibleProvider{BaseURL: "https://uncached.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{CandidateID: 1, Title: "Title", BodyText: "Body"}); err != nil {
		t.Fatal(err)
	}
	if len(client.payloads) != articleAssessmentSchemaMaxAttempts+1 {
		t.Fatalf("first article requests = %d", len(client.payloads))
	}
	for index := 0; index < articleAssessmentSchemaMaxAttempts; index++ {
		if requireMap(t, client.payloads[index]["response_format"])["type"] != articleAssessmentResponseSchemaMode {
			t.Fatalf("attempt %d format = %#v", index+1, client.payloads[index]["response_format"])
		}
	}
	objectPayload := client.payloads[articleAssessmentSchemaMaxAttempts]
	if requireMap(t, objectPayload["response_format"])["type"] != articleAssessmentResponseObjectMode {
		t.Fatalf("fallback format = %#v", objectPayload["response_format"])
	}
	user := lastUserMessage(t, objectPayload)
	if !strings.Contains(user, `unknown field "unused"`) || strings.Contains(user, "invalid chat json") {
		t.Fatalf("fallback user = %q", user)
	}
	invalidJSON := `{"qualityScore":80,"depthScore":70,"evergreenScore":60,"reasons":["ok","limit"],"summary":"ok","keywords":["a","b","c"],"unused":true}`
	for index := 1; index < len(client.payloads); index++ {
		messages := payloadMessages(t, client.payloads[index])
		if len(messages) != 3 {
			t.Fatalf("payload %d message count = %d, want original plus one error user", index, len(messages))
		}
		assertNoAssistantReplay(t, client.payloads[index], invalidJSON)
	}

	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{CandidateID: 2, Title: "Title", BodyText: "Body"}); err != nil {
		t.Fatal(err)
	}
	if len(client.payloads) != articleAssessmentSchemaMaxAttempts+2 {
		t.Fatalf("second article requests = %d", len(client.payloads))
	}
	if requireMap(t, client.payloads[articleAssessmentSchemaMaxAttempts+1]["response_format"])["type"] != articleAssessmentResponseSchemaMode {
		t.Fatalf("second article should probe schema again, format=%#v", client.payloads[articleAssessmentSchemaMaxAttempts+1]["response_format"])
	}
}

func TestArticleAssessmentFallsBackToJSONObjectAfterRetryableThenNonRetryableError(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	invalid := chatCompletionWithContent(`{"qualityScore":80,"depthScore":70,"evergreenScore":60,"reasons":["ok","limit"],"summary":"ok","keywords":["a","b","c"],"unused":true}`)
	valid := chatCompletionWithContent(articleAssessmentJSON(72, 64, 81, "specific evidence", "limited comparison"))
	client := &fakeOpenAIDoer{
		responses: []string{invalid, `{"error":{"message":"rate exceeded"}}`, valid},
		statuses:  []int{http.StatusOK, http.StatusTooManyRequests, http.StatusOK},
	}
	provider := OpenAICompatibleProvider{BaseURL: "https://rate-after-retry.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Body"}); err != nil {
		t.Fatal(err)
	}
	if len(client.payloads) != 3 {
		t.Fatalf("requests = %d, want schema, schema rate-limit, then json_object", len(client.payloads))
	}
	if requireMap(t, client.payloads[0]["response_format"])["type"] != articleAssessmentResponseSchemaMode {
		t.Fatalf("first format = %#v", client.payloads[0]["response_format"])
	}
	if requireMap(t, client.payloads[1]["response_format"])["type"] != articleAssessmentResponseSchemaMode {
		t.Fatalf("second format = %#v", client.payloads[1]["response_format"])
	}
	if requireMap(t, client.payloads[2]["response_format"])["type"] != articleAssessmentResponseObjectMode {
		t.Fatalf("fallback format = %#v", client.payloads[2]["response_format"])
	}
	user := lastUserMessage(t, client.payloads[2])
	if !strings.Contains(user, `unknown field "unused"`) || strings.Contains(user, "rate exceeded") {
		t.Fatalf("fallback should carry the last validator error, got %q", user)
	}
	assertNoAssistantReplay(t, client.payloads[2], `"unused":true`)
}

func TestArticleAssessmentFallsBackToJSONObjectAfterRetryableThenContextLimit(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	invalid := chatCompletionWithContent(`{"qualityScore":80,"depthScore":70,"evergreenScore":60,"reasons":["ok","limit"],"summary":"ok","keywords":["a","b","c"],"unused":true}`)
	valid := chatCompletionWithContent(articleAssessmentJSON(72, 64, 81, "specific evidence", "limited comparison"))
	client := &fakeOpenAIDoer{
		responses: []string{invalid, `{"error":{"message":"This model's maximum context length is 32768 tokens"}}`, valid},
		statuses:  []int{http.StatusOK, http.StatusBadRequest, http.StatusOK},
	}
	provider := OpenAICompatibleProvider{BaseURL: "https://context-after-retry.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Body"}); err != nil {
		t.Fatal(err)
	}
	if len(client.payloads) != 3 {
		t.Fatalf("requests = %d, want schema, context-limit, then json_object", len(client.payloads))
	}
	if requireMap(t, client.payloads[2]["response_format"])["type"] != articleAssessmentResponseObjectMode {
		t.Fatalf("fallback format = %#v", client.payloads[2]["response_format"])
	}
	messages := payloadMessages(t, client.payloads[2])
	if len(messages) != 3 {
		t.Fatalf("fallback messages = %#v", messages)
	}
	assertNoAssistantReplay(t, client.payloads[2], `"unused":true`)
}

func TestArticleAssessmentRetryCauseUsesMissingFieldNames(t *testing.T) {
	resetAssessmentOutputCapabilitiesForTest()
	missing := `{"qualityScore":50,"depthScore":50,"evergreenScore":50,"reasons":["one","two"]}`
	client := &fakeOpenAIDoer{responses: []string{
		chatCompletionWithContent(missing),
		chatCompletionWithContent(articleAssessmentJSON(50, 50, 50, "one", "two")),
	}}
	provider := OpenAICompatibleProvider{BaseURL: "https://missing.example", ChatModel: "MiMo-V2.5-Pro", HTTPClient: client}
	if _, err := provider.AssessArticle(context.Background(), ArticleAssessmentInput{Title: "Title", BodyText: "Body"}); err != nil {
		t.Fatal(err)
	}
	content := lastUserMessage(t, client.payloads[1])
	if !strings.Contains(content, "Missing required fields: summary, keywords.") {
		t.Fatalf("retry user = %q", content)
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

func assertChatSampling(t *testing.T, payload map[string]interface{}) {
	t.Helper()
	assertChatSamplingWithMaxTokens(t, payload, llmChatMaxTokens)
}

func assertChatSamplingWithMaxTokens(t *testing.T, payload map[string]interface{}, maxTokens int) {
	t.Helper()
	want := map[string]interface{}{
		"temperature":        llmChatTemperature,
		"top_p":              llmChatTopP,
		"top_k":              float64(llmChatTopK),
		"min_p":              llmChatMinP,
		"presence_penalty":   llmChatPresencePenalty,
		"repetition_penalty": llmChatRepetitionPenalty,
		"max_tokens":         float64(maxTokens),
	}
	for field, expected := range want {
		if payload[field] != expected {
			t.Fatalf("payload[%s] = %#v, want %#v", field, payload[field], expected)
		}
	}
}

func payloadMessages(t *testing.T, payload map[string]interface{}) []interface{} {
	t.Helper()
	messages, ok := payload["messages"].([]interface{})
	if !ok {
		t.Fatalf("messages = %#v", payload["messages"])
	}
	return messages
}

func lastUserMessage(t *testing.T, payload map[string]interface{}) string {
	t.Helper()
	var content string
	for _, raw := range payloadMessages(t, payload) {
		message := requireMap(t, raw)
		if message["role"] == "user" {
			text, _ := message["content"].(string)
			content = text
		}
	}
	if content == "" {
		t.Fatalf("missing user message in %#v", payload["messages"])
	}
	return content
}

func assertNoAssistantReplay(t *testing.T, payload map[string]interface{}, forbidden string) {
	t.Helper()
	for _, raw := range payloadMessages(t, payload) {
		message := requireMap(t, raw)
		content, _ := message["content"].(string)
		if message["role"] == "assistant" {
			t.Fatalf("retry replayed assistant content: %#v", message)
		}
		if forbidden != "" && strings.Contains(content, forbidden) {
			t.Fatalf("retry included previous completion %q in %q", forbidden, content)
		}
	}
}

func articleAssessmentJSON(quality int, depth int, evergreen int, reason1 string, reason2 string) string {
	payload, err := json.Marshal(map[string]interface{}{
		"qualityScore": quality, "depthScore": depth, "evergreenScore": evergreen,
		"reasons":  []string{reason1, reason2},
		"summary":  "The article explains a durable method with measurements.",
		"keywords": []string{"testing", "evidence", "methods"},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func chatCompletionWithContent(content string) string {
	payload, err := json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]string{"content": content}},
		},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func mustJSONString(value string) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(payload)
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
