package discovery

import (
	"strings"
	"testing"
)

func TestNormalizeArticleURLRemovesTrackingOnly(t *testing.T) {
	got, err := NormalizeArticleURL("HTTPS://Example.COM/post/../post/1?utm_source=x&fbclid=y&id=42#comments")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/post/1?id=42" {
		t.Fatalf("normalized url = %q", got)
	}
}

func TestContentHashNormalizesWhitespace(t *testing.T) {
	first := ContentHash("alpha   beta\n gamma")
	second := ContentHash("alpha beta gamma")
	if first == "" || first != second {
		t.Fatalf("hash mismatch: %q %q", first, second)
	}
}

func TestExtractArticleUsesDomDistiller(t *testing.T) {
	body := []byte(`<!doctype html>
<html>
  <head>
    <title>Fallback title</title>
    <meta property="og:title" content="Readable Title">
    <meta property="og:description" content="A useful summary">
    <meta property="og:url" content="https://example.com/articles/1">
  </head>
  <body>
    <nav>navigation</nav>
    <article>
      <h1>Readable Title</h1>
      <p>` + strings.Repeat("This is a paragraph with meaningful article content. ", 40) + `</p>
    </article>
  </body>
</html>`)
	article, err := ExtractArticle("https://example.com/articles/1?utm_source=x", body)
	if err != nil {
		t.Fatal(err)
	}
	if article.Title == "" || !strings.Contains(article.Text, "meaningful article content") {
		t.Fatalf("article extraction failed: %#v", article)
	}
	if article.Description != "A useful summary" || article.CanonicalURL != "https://example.com/articles/1" {
		t.Fatalf("metadata extraction failed: %#v", article)
	}
}
