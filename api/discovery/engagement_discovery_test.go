package discovery

import (
	"DataArk/archive"
	"DataArk/config"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestKeywordStatsAndArchiveRecommendations(t *testing.T) {
	setupSQLiteDB(t)
	rootDir := t.TempDir()
	oldRoot := config.ARCHIVEFILELOACTION
	config.ARCHIVEFILELOACTION = rootDir
	t.Cleanup(func() {
		config.ARCHIVEFILELOACTION = oldRoot
	})

	archiveDir := filepath.Join(rootDir, "example.com")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "article.html"), []byte("<html><title>Go Archive</title><body>searchable article</body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := archive.SaveArchiveDocumentDetails("example.com", "article.html", "https://example.com/article", "Go Archive", "searchable article"); err != nil {
		t.Fatal(err)
	}
	if err := archive.RecordSearchEvent("  Go   Archive ", 3); err != nil {
		t.Fatal(err)
	}
	if err := archive.RecordSearchEvent("Go Archive", 4); err != nil {
		t.Fatal(err)
	}

	keywords, err := archive.GetKeywordStats("go", "7d", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(keywords) != 1 || keywords[0].Keyword != "Go Archive" || keywords[0].Count != 2 {
		t.Fatalf("keywords = %#v", keywords)
	}

	if _, err := archive.RecordArchiveClick("/archive/example.com/article.html", "Go Archive"); err != nil {
		t.Fatal(err)
	}
	rankings, err := archive.GetArchiveRankings("7d", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rankings) != 1 || rankings[0].ClickCount != 1 || rankings[0].Title != "Go Archive" {
		t.Fatalf("rankings = %#v", rankings)
	}

	oldRecommender := archive.SwapArchiveRecommenderForTest(archive.ArchiveRecommenderFunc(func(_ context.Context, request archive.ArchiveRecommendRequest) (archive.ArchiveRecommendResponse, error) {
		if len(request.Documents) == 0 {
			return archive.ArchiveRecommendResponse{}, errors.New("empty pool")
		}
		return archive.ArchiveRecommendResponse{
			Items: []archive.ArchiveRecommendPick{{Path: request.Documents[0].Path, Reason: "测试归档推荐", Score: 0.8}},
			Model: "test-archive",
		}, nil
	}))
	t.Cleanup(oldRecommender)
	recommendations, err := archive.GetArchiveRecommendations(context.Background(), "7d", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations) != 1 || recommendations[0].Reason != "测试归档推荐" {
		t.Fatalf("recommendations = %#v", recommendations)
	}
}

func TestArchiveRankingsTolerateMissingDocumentMetadata(t *testing.T) {
	setupSQLiteDB(t)
	rootDir := t.TempDir()
	oldRoot := config.ARCHIVEFILELOACTION
	config.ARCHIVEFILELOACTION = rootDir
	t.Cleanup(func() {
		config.ARCHIVEFILELOACTION = oldRoot
	})

	archiveDir := filepath.Join(rootDir, "legacy.example")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "legacy.html"), []byte("<html><title>Legacy</title></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := archive.RecordArchiveClick("/archive/legacy.example/legacy.html", "legacy"); err != nil {
		t.Fatal(err)
	}
	rankings, err := archive.GetArchiveRankings("7d", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rankings) != 1 {
		t.Fatalf("rankings = %#v, want one item", rankings)
	}
	if rankings[0].Title != "legacy.html" || rankings[0].ClickCount != 1 {
		t.Fatalf("ranking fallback = %#v", rankings[0])
	}
}

func TestDiscoverySourceFetchAndCandidateState(t *testing.T) {
	setupSQLiteDB(t)
	oldFetcher := fetchDiscoveryRequest
	fetchDiscoveryRequest = func(_ context.Context, _ FetchRequest) (FetchResult, error) {
		return FetchResult{StatusCode: 200, ContentType: "application/rss+xml", Body: []byte(`<?xml version="1.0"?>
<rss version="2.0"><channel><item><title>First Post</title><link>https://example.com/posts/first</link><description>Useful summary</description><pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate></item></channel></rss>`)}, nil
	}
	t.Cleanup(func() {
		fetchDiscoveryRequest = oldFetcher
	})

	source, err := CreateDiscoverySource("Feed", "https://example.com/feed.xml", DiscoverySourceTypeFeed, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := FetchDiscoverySource(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 1 {
		t.Fatalf("fetch result = %#v", result)
	}
	if _, err := FetchDiscoverySource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	const userID = 901
	candidates, err := ListDiscoveryCandidatesForUser(userID, DiscoveryCandidateStatusNew, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Title != "First Post" {
		t.Fatalf("candidates = %#v", candidates)
	}
	if _, err := MarkUserCandidateRead(userID, candidates[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := MarkUserCandidateIgnored(userID, candidates[0].ID); err != nil {
		t.Fatal(err)
	}
	ignored, err := ListDiscoveryCandidatesForUser(userID, DiscoveryCandidateStatusIgnored, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ignored) != 1 {
		t.Fatalf("ignored candidates = %#v", ignored)
	}
}

func TestParseFeedCandidatesSupportsRSSAtomAndJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "rss",
			body: `<?xml version="1.0"?><rss version="2.0"><channel><item><title>RSS Post</title><link>https://example.com/rss-post</link><description>RSS summary</description></item></channel></rss>`,
			want: "RSS Post",
		},
		{
			name: "atom",
			body: `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Atom Post</title><link href="https://example.com/atom-post"/><summary>Atom summary</summary></entry></feed>`,
			want: "Atom Post",
		},
		{
			name: "rdf",
			body: `<?xml version="1.0"?><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/"><channel rdf:about="https://example.com/feed"><title>RDF Feed</title><link>https://example.com/</link><description>RDF feed</description></channel><item rdf:about="https://example.com/rdf"><title>RDF Post</title><link>https://example.com/rdf</link><description>RDF summary</description></item></rdf:RDF>`,
			want: "RDF Post",
		},
		{
			name: "json",
			body: `{"version":"https://jsonfeed.org/version/1.1","title":"JSON Feed","items":[{"id":"1","url":"https://example.com/json","title":"JSON Post","summary":"JSON summary"}]}`,
			want: "JSON Post",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			candidates, err := parseFeedCandidates([]byte(testCase.body))
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) != 1 || candidates[0].Title != testCase.want {
				t.Fatalf("candidates = %#v, want title %q", candidates, testCase.want)
			}
		})
	}
}
