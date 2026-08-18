package articlerules

import "testing"

func TestRejectURLBlocksListingsLoginsAndFeeds(t *testing.T) {
	cases := []struct {
		rawURL string
		anchor string
	}{
		{"https://example.com/tag/go", "Go"},
		{"https://example.com/tags/rust", ""},
		{"https://example.com/category/ops", "Ops"},
		{"https://example.com/login", "Sign in"},
		{"https://example.com/feed.xml", "RSS"},
		{"https://example.com/blogroll", "Friends"},
		{"https://example.com/posts?page=2", "Older"},
		{"https://example.com/", "Home"},
		{"https://example.com/about", "About"},
	}
	for _, testCase := range cases {
		reason, rejected := RejectURL(testCase.rawURL, testCase.anchor)
		if !rejected || reason == "" {
			t.Fatalf("%s should be rejected, got rejected=%v reason=%q", testCase.rawURL, rejected, reason)
		}
	}
}

func TestRejectURLAllowsArticlePaths(t *testing.T) {
	for _, rawURL := range []string{
		"https://example.com/posts/durable-notes",
		"https://example.com/2024/01/field-report.html",
		"https://notes.example.org/p/why-backpressure",
	} {
		if reason, rejected := RejectURL(rawURL, "A concrete article title"); rejected {
			t.Fatalf("%s should be admitted, reason=%q", rawURL, reason)
		}
	}
}

func TestRejectPageBlocksNonArticlesAndShortBodies(t *testing.T) {
	if reason, category, review := RejectPage(Page{
		URL: "https://example.com/tag/go", Title: "Tag: Go", Text: "Posts tagged Go", IsArticle: false, Language: "en",
	}, 100); !stringsHas(category, CategoryNotArticle) || review || reason == "" {
		t.Fatalf("tag page gate = %q %q review=%v", reason, category, review)
	}
	if _, category, _ := RejectPage(Page{
		URL: "https://example.com/login", Title: "Sign in", HasPasswordInput: true, IsArticle: false, Language: "en",
	}, 100); category != CategoryNotArticle {
		t.Fatalf("login page category = %q", category)
	}
	if _, category, _ := RejectPage(Page{
		URL: "https://example.com/posts/tiny", Title: "Tiny note", Text: "Too short.", IsArticle: true, Language: "en",
	}, 100); category != CategoryBodyTooShort {
		t.Fatalf("short body category = %q", category)
	}
	if _, category, review := RejectPage(Page{
		URL: "https://example.com/posts/ok", Title: "Ok", Text: "enough text for the language detector to stay unused", IsArticle: true,
	}, 10); category != CategoryLanguageUnknown || !review {
		t.Fatalf("unknown language = %q review=%v", category, review)
	}
}

func TestRejectPageAcceptsExtractedArticles(t *testing.T) {
	reason, category, review := RejectPage(Page{
		URL: "https://example.com/posts/1", Title: "First durable version",
		Text:     "This independently verifiable article presents evidence and a durable method.",
		Language: "en", IsArticle: true,
	}, 40)
	if reason != "" || category != "" || review {
		t.Fatalf("valid article rejected: %q %q review=%v", reason, category, review)
	}
}

func stringsHas(got, want string) bool { return got == want }
