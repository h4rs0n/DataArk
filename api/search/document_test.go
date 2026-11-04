package search

import (
	"DataArk/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeSnippetEscapesSourceAndKeepsOnlyGeneratedHighlight(t *testing.T) {
	snippet := safeSnippet(`<script>alert(1)</script>` + highlightStart + `a & "b"` + highlightEnd)
	if strings.Contains(snippet, "<script>") || !strings.Contains(snippet, "&lt;script&gt;") || !strings.Contains(snippet, `class="search-highlight"`) || !strings.Contains(snippet, "&amp;") {
		t.Fatal(snippet)
	}
}

func TestExistingArchiveEscapesAndChecksFiles(t *testing.T) {
	old := config.ARCHIVEFILELOACTION
	config.ARCHIVEFILELOACTION = t.TempDir()
	defer func() { config.ARCHIVEFILELOACTION = old }()
	dir := filepath.Join(config.ARCHIVEFILELOACTION, "example.org")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "saved #%?.html"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	got := existingArchive("example.org", "saved #%?.html")
	if got != "/archive/example.org/saved%20%23%25%3F.html" {
		t.Fatal(got)
	}
	for _, name := range []string{"missing.html", "../outside.html", "."} {
		if got := existingArchive("example.org", name); got != "" {
			t.Fatalf("invalid archive %q: %q", name, got)
		}
	}
}
