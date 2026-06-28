package recommendation

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeOpenAIDoer struct {
	responses []string
	paths     []string
}

func (fake *fakeOpenAIDoer) Do(req *http.Request) (*http.Response, error) {
	fake.paths = append(fake.paths, req.URL.Path)
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
		ChatModel:  "chat-model",
		HTTPClient: client,
	}
	enriched, err := provider.Enrich(context.Background(), EnrichmentInput{Title: "Go article"})
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Summary != "Short" || enriched.Topics[0] != "Go" || enriched.Model != "chat-model" {
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
	if len(reranked.Items) != 1 || reranked.Items[0].CandidateID != 2 || reranked.Model != "chat-model" {
		t.Fatalf("reranked = %#v", reranked)
	}
	if len(client.paths) != 2 || client.paths[0] != "/v1/chat/completions" || client.paths[1] != "/v1/chat/completions" {
		t.Fatalf("paths = %#v", client.paths)
	}
}
