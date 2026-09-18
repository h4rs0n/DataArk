package archive

import (
	"context"
	"errors"
	"testing"
)

func TestGetArchiveRecommendationsRequiresLLMAndCaches(t *testing.T) {
	setupSQLiteDB(t)
	resetArchiveRecommendCacheForTest()
	t.Cleanup(resetArchiveRecommendCacheForTest)
	if err := SaveArchiveDocumentDetails("example.com", "article.html", "https://example.com/article", "Go Archive", "searchable article"); err != nil {
		t.Fatal(err)
	}

	calls := 0
	restore := SwapArchiveRecommenderForTest(ArchiveRecommenderFunc(func(_ context.Context, request ArchiveRecommendRequest) (ArchiveRecommendResponse, error) {
		calls++
		return ArchiveRecommendResponse{
			Items: []ArchiveRecommendPick{{Path: request.Documents[0].Path, Reason: "模型推荐", Score: 0.7}},
			Model: "fake",
		}, nil
	}))
	t.Cleanup(restore)

	first, err := GetArchiveRecommendations(context.Background(), "7d", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Reason != "模型推荐" || calls != 1 {
		t.Fatalf("first = %#v calls=%d", first, calls)
	}
	second, err := GetArchiveRecommendations(context.Background(), "7d", 5)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || second[0].Reason != first[0].Reason {
		t.Fatalf("cache miss: calls=%d second=%#v", calls, second)
	}
}

func TestGetArchiveRecommendationsFailsWithoutKeywordFallback(t *testing.T) {
	setupSQLiteDB(t)
	resetArchiveRecommendCacheForTest()
	t.Cleanup(resetArchiveRecommendCacheForTest)
	if err := SaveArchiveDocumentDetails("example.com", "article.html", "https://example.com/article", "Go Archive", "searchable article"); err != nil {
		t.Fatal(err)
	}
	restore := SwapArchiveRecommenderForTest(ArchiveRecommenderFunc(func(context.Context, ArchiveRecommendRequest) (ArchiveRecommendResponse, error) {
		return ArchiveRecommendResponse{}, errors.New("model down")
	}))
	t.Cleanup(restore)
	items, err := GetArchiveRecommendations(context.Background(), "7d", 5)
	if err == nil || items != nil {
		t.Fatalf("expected failure, got %#v %v", items, err)
	}
}

func TestGetArchiveRecommendationsRejectsEmptyReasons(t *testing.T) {
	setupSQLiteDB(t)
	resetArchiveRecommendCacheForTest()
	t.Cleanup(resetArchiveRecommendCacheForTest)
	if err := SaveArchiveDocumentDetails("example.com", "article.html", "https://example.com/article", "Go Archive", "searchable article"); err != nil {
		t.Fatal(err)
	}
	restore := SwapArchiveRecommenderForTest(ArchiveRecommenderFunc(func(_ context.Context, request ArchiveRecommendRequest) (ArchiveRecommendResponse, error) {
		return ArchiveRecommendResponse{Items: []ArchiveRecommendPick{{Path: request.Documents[0].Path, Reason: "", Score: 1}}}, nil
	}))
	t.Cleanup(restore)
	if _, err := GetArchiveRecommendations(context.Background(), "7d", 5); err == nil {
		t.Fatal("missing reason should fail")
	}
}
