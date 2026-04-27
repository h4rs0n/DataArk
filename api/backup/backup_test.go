package backup

import (
	"DataArk/common"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeZipEntryPathRejectsTraversal(t *testing.T) {
	cases := []string{
		"../database.sql",
		"/backup/database.sql",
		"backup/../../database.sql",
		`backup\..\database.sql`,
	}

	for _, entry := range cases {
		if _, ok := safeZipEntryPath(entry); ok {
			t.Fatalf("expected %q to be rejected", entry)
		}
	}
}

func TestSafeZipEntryPathAcceptsBackupFiles(t *testing.T) {
	got, ok := safeZipEntryPath("backup/archive/example.com/page.html")
	if !ok {
		t.Fatal("expected backup archive path to be accepted")
	}

	want := filepath.Join("backup", "archive", "example.com", "page.html")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDiscoverRestoreComponents(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backup")
	if err := os.MkdirAll(filepath.Join(backupRoot, "archive", "example.com"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupRoot, "database.sql"), []byte("-- sql"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupRoot, "20260427.dump"), []byte("dump"), 0o644); err != nil {
		t.Fatal(err)
	}

	components, err := discoverRestoreComponents(root)
	if err != nil {
		t.Fatal(err)
	}

	if components.DatabasePath != filepath.Join(backupRoot, "database.sql") {
		t.Fatalf("unexpected database path %q", components.DatabasePath)
	}
	if components.MeiliDumpPath != filepath.Join(backupRoot, "20260427.dump") {
		t.Fatalf("unexpected dump path %q", components.MeiliDumpPath)
	}
	if components.ArchiveDir != filepath.Join(backupRoot, "archive") {
		t.Fatalf("unexpected archive dir %q", components.ArchiveDir)
	}
}

func TestReplaceArchiveDirKeepsArchiveRoot(t *testing.T) {
	oldArchiveLocation := common.ARCHIVEFILELOACTION
	t.Cleanup(func() {
		common.ARCHIVEFILELOACTION = oldArchiveLocation
	})

	root := t.TempDir()
	archiveRoot := filepath.Join(root, "archive")
	sourceArchive := filepath.Join(root, "backup", "archive")
	tempRoot := filepath.Join(root, "restore-temp")

	common.ARCHIVEFILELOACTION = archiveRoot

	if err := os.MkdirAll(filepath.Join(archiveRoot, "old.example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveRoot, "old.example", "old.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(archiveRoot)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(sourceArchive, "new.example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceArchive, "new.example", "new.html"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := replaceArchiveDir(sourceArchive, tempRoot); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(archiveRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("archive root directory should be preserved during restore")
	}
	if _, err := os.Stat(filepath.Join(archiveRoot, "old.example", "old.html")); !os.IsNotExist(err) {
		t.Fatalf("expected old archive file to be removed, got %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(archiveRoot, "new.example", "new.html")); err != nil || string(got) != "new" {
		t.Fatalf("expected restored archive file, got content %q err %v", string(got), err)
	}
}
