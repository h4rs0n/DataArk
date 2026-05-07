package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractHTMLTextRemovesScriptStyleAndTags(t *testing.T) {
	text, err := ExtractHTMLText(`<html><head><style>.x{}</style><script>alert(1)</script></head><body><h1>Hello</h1><p>World</p></body></html>`)
	if err != nil {
		t.Fatalf("ExtractHTMLText returned error: %v", err)
	}
	if text != "HelloWorld" {
		t.Fatalf("text = %q, want HelloWorld", text)
	}
}

func TestGetHTMLTitle(t *testing.T) {
	title, err := GetHTMLTitle("<html><title>Example</title></html>")
	if err != nil {
		t.Fatalf("GetHTMLTitle returned error: %v", err)
	}
	if title != "Example" {
		t.Fatalf("title = %q, want Example", title)
	}
	if _, err := GetHTMLTitle("<html></html>"); err == nil {
		t.Fatal("missing title should return error")
	}
}

func TestGetHTMLFileContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte("<html>file</html>"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	content, err := GetHTMLFileContent(path)
	if err != nil {
		t.Fatalf("GetHTMLFileContent returned error: %v", err)
	}
	if !strings.Contains(content, "file") {
		t.Fatalf("content = %q, want file", content)
	}
	if _, err := GetHTMLFileContent(filepath.Join(t.TempDir(), "missing.html")); err == nil {
		t.Fatal("missing file should return error")
	}
}
