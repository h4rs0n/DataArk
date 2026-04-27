package search

import (
	"DataArk/common"
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveArchiveDocumentPath(t *testing.T) {
	rootDir := t.TempDir()
	oldRoot := common.ARCHIVEFILELOACTION
	common.ARCHIVEFILELOACTION = rootDir
	t.Cleanup(func() {
		common.ARCHIVEFILELOACTION = oldRoot
	})

	got, err := resolveArchiveDocumentPath("/archive/example.com/saved%20page.html")
	if err != nil {
		t.Fatalf("resolveArchiveDocumentPath returned error: %v", err)
	}

	expectedPath := filepath.Join(rootDir, "example.com", "saved page.html")
	if got.Domain != "example.com" {
		t.Fatalf("Domain = %q, want %q", got.Domain, "example.com")
	}
	if got.Filename != "saved page.html" {
		t.Fatalf("Filename = %q, want %q", got.Filename, "saved page.html")
	}
	if got.AbsPath != expectedPath {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, expectedPath)
	}
}

func TestResolveArchiveDocumentPathRejectsTraversal(t *testing.T) {
	oldRoot := common.ARCHIVEFILELOACTION
	common.ARCHIVEFILELOACTION = t.TempDir()
	t.Cleanup(func() {
		common.ARCHIVEFILELOACTION = oldRoot
	})

	_, err := resolveArchiveDocumentPath("/archive/example.com/../secret.html")
	if !errors.Is(err, ErrInvalidArchivePath) {
		t.Fatalf("err = %v, want ErrInvalidArchivePath", err)
	}
}
