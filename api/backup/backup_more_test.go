package backup

import (
	"DataArk/common"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreparedBackupCleanupAndWriteZip(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	prepared := &PreparedBackup{RootDir: root, BackupDir: backupDir, FileName: "backup.zip"}
	var buffer bytes.Buffer
	if err := prepared.WriteZip(&buffer); err != nil {
		t.Fatalf("WriteZip returned error: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("zip reader error: %v", err)
	}
	if len(reader.File) == 0 {
		t.Fatal("zip should contain files")
	}

	prepared.Cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("root should be removed, stat err = %v", err)
	}
	(*PreparedBackup)(nil).Cleanup()
	if err := (*PreparedBackup)(nil).WriteZip(io.Discard); err == nil {
		t.Fatal("nil PreparedBackup WriteZip should return error")
	}
}

func TestWriteJSONAndExtractZip(t *testing.T) {
	root := t.TempDir()
	jsonPath := filepath.Join(root, "manifest.json")
	if err := writeJSON(jsonPath, Manifest{DatabaseSQLFile: "database.sql"}); err != nil {
		t.Fatalf("writeJSON returned error: %v", err)
	}
	var manifest Manifest
	content, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if manifest.DatabaseSQLFile != "database.sql" {
		t.Fatalf("manifest = %#v", manifest)
	}

	zipPath := filepath.Join(root, "backup.zip")
	createTestZip(t, zipPath, map[string]string{
		"backup/database.sql":              "-- sql",
		"backup/archive/example/page.html": "<html></html>",
	})
	extractDir := filepath.Join(root, "extract")
	if err := extractZip(zipPath, extractDir); err != nil {
		t.Fatalf("extractZip returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractDir, "backup", "database.sql")); err != nil {
		t.Fatalf("expected extracted file: %v", err)
	}
}

func TestExtractZipRejectsUnsafeEntry(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "unsafe.zip")
	createTestZip(t, zipPath, map[string]string{"../escape.txt": "bad"})
	if err := extractZip(zipPath, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unsafe zip entry") {
		t.Fatalf("err = %v, want unsafe zip entry", err)
	}
}

func TestBackupFileHelpers(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "file.txt"), []byte("content"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(source, destination); err != nil {
		t.Fatalf("copyDir returned error: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "nested", "file.txt")); err != nil || string(got) != "content" {
		t.Fatalf("copied content = %q err=%v", string(got), err)
	}
	if err := copyFile(source, filepath.Join(root, "bad")); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("copyFile directory err = %v", err)
	}
	if err := removeDirContents(destination); err != nil {
		t.Fatalf("removeDirContents returned error: %v", err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("destination entries = %#v, want empty", entries)
	}
}

func TestArchiveSnapshotAndDumpRestoreHelpers(t *testing.T) {
	oldArchiveRoot := common.ARCHIVEFILELOACTION
	oldDumpDir := common.MEILIDumpDir
	t.Cleanup(func() {
		common.ARCHIVEFILELOACTION = oldArchiveRoot
		common.MEILIDumpDir = oldDumpDir
	})

	root := t.TempDir()
	common.ARCHIVEFILELOACTION = filepath.Join(root, "missing-archive")
	destination := filepath.Join(root, "snapshot")
	if err := copyArchiveSnapshot(destination); err != nil {
		t.Fatalf("copyArchiveSnapshot missing source returned error: %v", err)
	}
	if info, err := os.Stat(destination); err != nil || !info.IsDir() {
		t.Fatalf("destination should be directory: info=%#v err=%v", info, err)
	}

	dumpSource := filepath.Join(root, "source.dump")
	if err := os.WriteFile(dumpSource, []byte("dump"), 0o644); err != nil {
		t.Fatal(err)
	}
	common.MEILIDumpDir = filepath.Join(root, "dumps")
	restored, err := restoreMeiliDumpFile(dumpSource)
	if err != nil {
		t.Fatalf("restoreMeiliDumpFile returned error: %v", err)
	}
	if !strings.HasPrefix(restored, "restored-") {
		t.Fatalf("restored file name = %q", restored)
	}
	if _, err := os.Stat(filepath.Join(common.MEILIDumpDir, restored)); err != nil {
		t.Fatalf("restored dump missing: %v", err)
	}
}

func TestSmallBackupHelpers(t *testing.T) {
	if permissionOrDefault(0o640, 0o600) != 0o640 {
		t.Fatal("permissionOrDefault should prefer explicit permission")
	}
	if permissionOrDefault(0, 0o600) != 0o600 {
		t.Fatal("permissionOrDefault should use fallback")
	}
	if pathDepth("/tmp/root", "/tmp/root") != 0 {
		t.Fatal("pathDepth root should be zero")
	}
	if !isSubpath("/tmp/root", "/tmp/root/nested/file") {
		t.Fatal("nested path should be subpath")
	}
	if isSubpath("/tmp/root", "/tmp/other") {
		t.Fatal("outside path should not be subpath")
	}
	if chooseRestoreCandidate("/tmp/root", []string{"/tmp/root/deep/a.sql", "/tmp/root/database.sql"}, "database.sql") != "/tmp/root/database.sql" {
		t.Fatal("preferred shallow candidate should be selected")
	}
}

func TestWaitForFile(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "ready.txt")
	if err := os.WriteFile(filePath, []byte("ready"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := waitForFile(context.Background(), filePath, time.Second); err != nil {
		t.Fatalf("waitForFile existing file returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForFile(ctx, filepath.Join(root, "missing.txt"), time.Second); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForFile canceled err = %v", err)
	}
}

func TestRunDatabaseCommandError(t *testing.T) {
	err := runDatabaseCommand(context.Background(), "definitely-not-a-real-dataark-command")
	if err == nil || !strings.Contains(err.Error(), "definitely-not-a-real-dataark-command failed") {
		t.Fatalf("err = %v, want command failure", err)
	}
}

func createTestZip(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(file)
	for name, content := range files {
		writer, err := zipWriter.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
