package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanArchiveStats(t *testing.T) {
	rootDir := t.TempDir()

	writeFile := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			t.Fatalf("failed to create test dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("<html></html>"), 0o644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	writeFile(filepath.Join(rootDir, "example.com", "one.html"))
	writeFile(filepath.Join(rootDir, "example.com", "nested", "two.HTML"))
	writeFile(filepath.Join(rootDir, "example.com", "ignored.txt"))
	writeFile(filepath.Join(rootDir, "news.example", "article.html"))
	writeFile(filepath.Join(rootDir, "Temporary", "upload.html"))
	writeFile(filepath.Join(rootDir, "root.html"))

	stats, err := ScanArchiveStats(rootDir)
	if err != nil {
		t.Fatalf("ScanArchiveStats returned error: %v", err)
	}

	if len(stats) != 2 {
		t.Fatalf("expected 2 source stats, got %d: %#v", len(stats), stats)
	}
	if stats[0].Source != "example.com" || stats[0].FileCount != 2 {
		t.Fatalf("unexpected first source stat: %#v", stats[0])
	}
	if stats[1].Source != "news.example" || stats[1].FileCount != 1 {
		t.Fatalf("unexpected second source stat: %#v", stats[1])
	}
}
