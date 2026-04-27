package backup

import (
	"DataArk/common"
	"DataArk/search"
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/meilisearch/meilisearch-go"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	meiliDumpTimeout       = 10 * time.Minute
	meiliDumpFileTimeout   = 15 * time.Second
	databaseDumpTimeout    = 10 * time.Minute
	databaseRestoreTimeout = 10 * time.Minute
)

var operationMu sync.Mutex

type PreparedBackup struct {
	RootDir   string
	BackupDir string
	FileName  string
	Manifest  Manifest
}

type Manifest struct {
	CreatedAt       string `json:"createdAt"`
	MeiliDumpFile   string `json:"meiliDumpFile"`
	MeiliDumpUID    string `json:"meiliDumpUid"`
	DatabaseSQLFile string `json:"databaseSqlFile"`
	ArchiveDir      string `json:"archiveDir"`
}

type RestoreResult struct {
	MeiliDumpFile     string `json:"meiliDumpFile"`
	DatabaseRestored  bool   `json:"databaseRestored"`
	ArchiveRestored   bool   `json:"archiveRestored"`
	IndexedDocuments  int    `json:"indexedDocuments"`
	RefreshedStatRows int    `json:"refreshedStatRows"`
}

type restoreComponents struct {
	MeiliDumpPath string
	DatabasePath  string
	ArchiveDir    string
}

func CreateBackup(ctx context.Context) (*PreparedBackup, error) {
	operationMu.Lock()
	defer operationMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	createdAt := time.Now()
	tempRoot, err := os.MkdirTemp("", "dataark-backup-*")
	if err != nil {
		return nil, err
	}

	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = os.RemoveAll(tempRoot)
		}
	}()

	backupDir := filepath.Join(tempRoot, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, err
	}

	meiliDumpFile, meiliDumpUID, err := createMeiliDump(ctx, backupDir)
	if err != nil {
		return nil, err
	}

	databaseFile := "database.sql"
	if err := createDatabaseDump(ctx, filepath.Join(backupDir, databaseFile)); err != nil {
		return nil, err
	}

	if err := copyArchiveSnapshot(filepath.Join(backupDir, "archive")); err != nil {
		return nil, err
	}

	manifest := Manifest{
		CreatedAt:       createdAt.Format(time.RFC3339),
		MeiliDumpFile:   meiliDumpFile,
		MeiliDumpUID:    meiliDumpUID,
		DatabaseSQLFile: databaseFile,
		ArchiveDir:      "archive",
	}
	if err := writeJSON(filepath.Join(backupDir, "manifest.json"), manifest); err != nil {
		return nil, err
	}

	cleanupOnError = false
	return &PreparedBackup{
		RootDir:   tempRoot,
		BackupDir: backupDir,
		FileName:  "dataark-backup-" + createdAt.Format("20060102-150405") + ".zip",
		Manifest:  manifest,
	}, nil
}

func (p *PreparedBackup) Cleanup() {
	if p == nil || p.RootDir == "" {
		return
	}
	_ = os.RemoveAll(p.RootDir)
}

func (p *PreparedBackup) WriteZip(w io.Writer) error {
	if p == nil {
		return errors.New("backup is not prepared")
	}

	zipWriter := zip.NewWriter(w)
	if err := addDirectoryToZip(zipWriter, p.BackupDir); err != nil {
		_ = zipWriter.Close()
		return err
	}
	return zipWriter.Close()
}

func RestoreBackup(ctx context.Context, zipPath string) (*RestoreResult, error) {
	operationMu.Lock()
	defer operationMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tempRoot, err := os.MkdirTemp("", "dataark-restore-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempRoot)

	extractDir := filepath.Join(tempRoot, "extract")
	if err := extractZip(zipPath, extractDir); err != nil {
		return nil, err
	}

	components, err := discoverRestoreComponents(extractDir)
	if err != nil {
		return nil, err
	}

	restoredDumpFile, err := restoreMeiliDumpFile(components.MeiliDumpPath)
	if err != nil {
		return nil, err
	}

	if err := restoreDatabaseDump(ctx, components.DatabasePath); err != nil {
		return nil, err
	}

	if err := replaceArchiveDir(components.ArchiveDir, tempRoot); err != nil {
		return nil, err
	}

	// Meilisearch dump import is a startup-only Meilisearch operation. For this
	// running API restore path, rebuild the application index from restored HTML.
	rebuildResult, err := search.RebuildIndexFromArchive(ctx)
	if err != nil {
		return nil, err
	}

	stats, err := common.RefreshArchiveStatsFromDisk()
	if err != nil {
		return nil, err
	}

	return &RestoreResult{
		MeiliDumpFile:     restoredDumpFile,
		DatabaseRestored:  true,
		ArchiveRestored:   true,
		IndexedDocuments:  rebuildResult.Documents,
		RefreshedStatRows: len(stats.Sources),
	}, nil
}

func createMeiliDump(ctx context.Context, backupDir string) (string, string, error) {
	client := meilisearch.New(common.MEILIHOST, meilisearch.WithAPIKey(common.MEILIAPIKey))
	taskInfo, err := client.CreateDumpWithContext(ctx)
	if err != nil {
		return "", "", fmt.Errorf("create meilisearch dump: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, meiliDumpTimeout)
	defer cancel()

	task, err := client.WaitForTaskWithContext(waitCtx, taskInfo.TaskUID, time.Second)
	if err != nil {
		return "", "", fmt.Errorf("wait meilisearch dump task: %w", err)
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return "", "", fmt.Errorf("meilisearch dump task %d finished with status %s", taskInfo.TaskUID, task.Status)
	}

	dumpUID := strings.TrimSpace(task.Details.DumpUid)
	if dumpUID == "" {
		return "", "", fmt.Errorf("meilisearch dump task %d did not return dump uid", taskInfo.TaskUID)
	}

	sourcePath := filepath.Join(common.MEILIDumpDir, dumpUID+".dump")
	if err := waitForFile(waitCtx, sourcePath, meiliDumpFileTimeout); err != nil {
		return "", "", fmt.Errorf("find meilisearch dump file %s: %w", sourcePath, err)
	}

	dumpFileName := dumpUID + ".dump"
	if err := copyFile(sourcePath, filepath.Join(backupDir, dumpFileName)); err != nil {
		return "", "", fmt.Errorf("copy meilisearch dump: %w", err)
	}
	return dumpFileName, dumpUID, nil
}

func createDatabaseDump(ctx context.Context, destination string) error {
	if strings.TrimSpace(common.DBName) == "" {
		return errors.New("database name is empty")
	}

	dumpCtx, cancel := context.WithTimeout(ctx, databaseDumpTimeout)
	defer cancel()

	args := []string{
		"--host", common.DBHost,
		"--port", common.DBPort,
		"--username", common.DBUser,
		"--dbname", common.DBName,
		"--format", "plain",
		"--clean",
		"--if-exists",
		"--no-owner",
		"--no-privileges",
		"--file", destination,
	}

	return runDatabaseCommand(dumpCtx, "pg_dump", args...)
}

func restoreDatabaseDump(ctx context.Context, sqlPath string) error {
	restoreCtx, cancel := context.WithTimeout(ctx, databaseRestoreTimeout)
	defer cancel()

	args := []string{
		"--host", common.DBHost,
		"--port", common.DBPort,
		"--username", common.DBUser,
		"--dbname", common.DBName,
		"--set", "ON_ERROR_STOP=on",
		"--single-transaction",
		"--file", sqlPath,
	}

	return runDatabaseCommand(restoreCtx, "psql", args...)
}

func runDatabaseCommand(ctx context.Context, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+common.DBPassword)

	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s failed: %s", command, message)
	}
	return nil
}

func copyArchiveSnapshot(destination string) error {
	source := strings.TrimSpace(common.ARCHIVEFILELOACTION)
	if source == "" {
		return errors.New("archive location is empty")
	}

	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(destination, 0o755)
		}
		return err
	}

	return copyDir(source, destination)
}

func restoreMeiliDumpFile(source string) (string, error) {
	if err := os.MkdirAll(common.MEILIDumpDir, 0o755); err != nil {
		return "", err
	}

	fileName := "restored-" + time.Now().Format("20060102-150405") + "-" + filepath.Base(source)
	destination := filepath.Join(common.MEILIDumpDir, fileName)
	if err := copyFile(source, destination); err != nil {
		return "", err
	}
	return fileName, nil
}

func replaceArchiveDir(sourceArchive string, tempRoot string) error {
	archiveRoot, err := cleanArchiveRoot()
	if err != nil {
		return err
	}

	sourceInfo, err := os.Stat(sourceArchive)
	if err != nil {
		return err
	}
	if !sourceInfo.IsDir() {
		return fmt.Errorf("backup archive path %s is not a directory", sourceArchive)
	}

	backupCurrent := filepath.Join(tempRoot, "current-archive")
	hasCurrentArchive := false
	if archiveInfo, err := os.Stat(archiveRoot); err == nil {
		if !archiveInfo.IsDir() {
			return fmt.Errorf("archive location %s is not a directory", archiveRoot)
		}
		if err := copyDir(archiveRoot, backupCurrent); err != nil {
			return err
		}
		hasCurrentArchive = true
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := removeDirContents(archiveRoot); err != nil {
		return err
	}
	if err := copyDir(sourceArchive, archiveRoot); err != nil {
		_ = removeDirContents(archiveRoot)
		if hasCurrentArchive {
			_ = copyDir(backupCurrent, archiveRoot)
		}
		return err
	}
	return nil
}

func removeDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func cleanArchiveRoot() (string, error) {
	root := strings.TrimSpace(common.ARCHIVEFILELOACTION)
	if root == "" {
		return "", errors.New("archive location is empty")
	}

	cleaned := filepath.Clean(root)
	if cleaned == "." || cleaned == string(os.PathSeparator) {
		return "", fmt.Errorf("refuse to replace unsafe archive location %q", root)
	}
	return cleaned, nil
}

func writeJSON(destination string, value interface{}) error {
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func addDirectoryToZip(zipWriter *zip.Writer, sourceDir string) error {
	sourceParent := filepath.Dir(sourceDir)

	return filepath.WalkDir(sourceDir, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		relativePath, err := filepath.Rel(sourceParent, currentPath)
		if err != nil {
			return err
		}
		zipName := filepath.ToSlash(relativePath)
		if entry.IsDir() {
			zipName += "/"
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = zipName
		if !entry.IsDir() {
			header.Method = zip.Deflate
		}

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		file, err := os.Open(currentPath)
		if err != nil {
			return err
		}

		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func extractZip(source string, destination string) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		relativePath, ok := safeZipEntryPath(file.Name)
		if !ok {
			return fmt.Errorf("unsafe zip entry %q", file.Name)
		}

		targetPath := filepath.Join(destination, relativePath)
		if !isSubpath(destination, targetPath) {
			return fmt.Errorf("unsafe zip entry %q", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, permissionOrDefault(file.Mode(), 0o755)); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		sourceFile, err := file.Open()
		if err != nil {
			return err
		}

		targetFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, permissionOrDefault(file.Mode(), 0o644))
		if err != nil {
			_ = sourceFile.Close()
			return err
		}

		_, copyErr := io.Copy(targetFile, sourceFile)
		closeErr := targetFile.Close()
		_ = sourceFile.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}

	return nil
}

func discoverRestoreComponents(root string) (*restoreComponents, error) {
	var components restoreComponents
	var archiveDepth int
	var dumpCandidates []string
	var databaseCandidates []string

	err := filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == root {
			return nil
		}

		name := strings.ToLower(entry.Name())
		if entry.IsDir() {
			if name == "archive" {
				depth := pathDepth(root, currentPath)
				if components.ArchiveDir == "" || depth < archiveDepth {
					components.ArchiveDir = currentPath
					archiveDepth = depth
				}
			}
			return nil
		}

		if strings.HasSuffix(name, ".dump") {
			dumpCandidates = append(dumpCandidates, currentPath)
		}
		if strings.HasSuffix(name, ".sql") {
			databaseCandidates = append(databaseCandidates, currentPath)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	components.MeiliDumpPath = chooseRestoreCandidate(root, dumpCandidates, "")
	components.DatabasePath = chooseRestoreCandidate(root, databaseCandidates, "database.sql")

	if components.MeiliDumpPath == "" {
		return nil, errors.New("backup zip does not contain a meilisearch .dump file")
	}
	if components.DatabasePath == "" {
		return nil, errors.New("backup zip does not contain a database .sql file")
	}
	if components.ArchiveDir == "" {
		return nil, errors.New("backup zip does not contain an archive directory")
	}

	return &components, nil
}

func chooseRestoreCandidate(root string, candidates []string, preferredName string) string {
	if len(candidates) == 0 {
		return ""
	}

	sort.Slice(candidates, func(left int, right int) bool {
		leftName := strings.ToLower(filepath.Base(candidates[left]))
		rightName := strings.ToLower(filepath.Base(candidates[right]))
		if preferredName != "" && leftName != rightName {
			preferred := strings.ToLower(preferredName)
			if leftName == preferred {
				return true
			}
			if rightName == preferred {
				return false
			}
		}

		leftDepth := pathDepth(root, candidates[left])
		rightDepth := pathDepth(root, candidates[right])
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return candidates[left] < candidates[right]
	})

	return candidates[0]
}

func safeZipEntryPath(name string) (string, bool) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", false
		}
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return filepath.FromSlash(cleaned), true
}

func permissionOrDefault(mode fs.FileMode, fallback fs.FileMode) fs.FileMode {
	if perm := mode.Perm(); perm != 0 {
		return perm
	}
	return fallback
}

func pathDepth(root string, target string) int {
	relativePath, err := filepath.Rel(root, target)
	if err != nil || relativePath == "." {
		return 0
	}
	return len(strings.Split(filepath.ToSlash(relativePath), "/"))
}

func waitForFile(ctx context.Context, filePath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := os.Stat(filePath)
		if err == nil && !info.IsDir() {
			return nil
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return os.ErrNotExist
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func copyDir(source string, destination string) error {
	return filepath.WalkDir(source, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		relativePath, err := filepath.Rel(source, currentPath)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(destination, relativePath)

		if entry.IsDir() {
			return os.MkdirAll(targetPath, info.Mode().Perm())
		}
		return copyFile(currentPath, targetPath)
	})
}

func copyFile(source string, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", source)
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}

	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	targetFile, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(targetFile, sourceFile)
	closeErr := targetFile.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func isSubpath(root string, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	relativePath, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	return relativePath == "." || (!strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) && relativePath != "..")
}
