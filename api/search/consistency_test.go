package search

import (
	"DataArk/common"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeArchiveIndexStore struct {
	documents         []archiveIndexDocument
	afterRebuildDocs  []archiveIndexDocument
	rebuildDocuments  int
	rebuildIssues     []ArchiveConsistencyIssue
	rebuildErr        error
	rebuildWasInvoked bool
}

func (f *fakeArchiveIndexStore) ListArchiveDocuments(context.Context) ([]archiveIndexDocument, error) {
	if f.rebuildWasInvoked && f.afterRebuildDocs != nil {
		return f.afterRebuildDocs, nil
	}
	return f.documents, nil
}

func (f *fakeArchiveIndexStore) RebuildArchiveIndex(context.Context) (int, []ArchiveConsistencyIssue, error) {
	f.rebuildWasInvoked = true
	return f.rebuildDocuments, f.rebuildIssues, f.rebuildErr
}

type fakeArchiveStatsStore struct {
	stats             *common.ArchiveStatsSnapshot
	refreshedStats    *common.ArchiveStatsSnapshot
	refreshWasInvoked bool
}

func (f *fakeArchiveStatsStore) GetArchiveStats() (*common.ArchiveStatsSnapshot, error) {
	if f.refreshWasInvoked && f.refreshedStats != nil {
		return f.refreshedStats, nil
	}
	return f.stats, nil
}

func (f *fakeArchiveStatsStore) RefreshArchiveStats() (*common.ArchiveStatsSnapshot, error) {
	f.refreshWasInvoked = true
	return f.refreshedStats, nil
}

func TestArchiveConsistencyCheckFindsRecoverableAndUnrecoverableIssues(t *testing.T) {
	root := t.TempDir()
	writeArchiveHTML(t, root, "example.com", "page.html", "Page", "indexed")
	writeArchiveHTML(t, root, "example.com", "missing-index.html", "Missing", "needs index")
	writeFile(t, filepath.Join(root, "broken.example", "bad.html"), "<html><body>no title</body></html>")

	index := &fakeArchiveIndexStore{
		documents: []archiveIndexDocument{
			{ID: "doc-1", Domain: "example.com", Filename: "page.html"},
			{ID: "doc-2", Domain: "example.com", Filename: "page.html"},
			{ID: "orphan", Domain: "old.example", Filename: "lost.html"},
		},
	}
	stats := &fakeArchiveStatsStore{
		stats: &common.ArchiveStatsSnapshot{
			TotalFiles: 1,
			Sources: []common.ArchiveStatItem{
				{Source: "example.com", FileCount: 1},
				{Source: "old.example", FileCount: 4},
			},
		},
	}
	service := archiveConsistencyService{
		archiveRoot: root,
		index:       index,
		stats:       stats,
		now: func() time.Time {
			return time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
		},
	}

	report, err := service.Check(context.Background())
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}

	if report.Consistent {
		t.Fatal("report should not be consistent")
	}
	if report.HTMLFiles != 3 {
		t.Fatalf("HTMLFiles = %d, want 3", report.HTMLFiles)
	}
	if report.MeiliDocuments != 3 {
		t.Fatalf("MeiliDocuments = %d, want 3", report.MeiliDocuments)
	}
	if report.DatabaseStatTotal != 1 {
		t.Fatalf("DatabaseStatTotal = %d, want 1", report.DatabaseStatTotal)
	}
	assertIssue(t, report.RecoverableIssues, ArchiveConsistencyStoreMeili, "missing-index.html", "搜索索引缺失")
	assertIssue(t, report.RecoverableIssues, ArchiveConsistencyStoreMeili, "page.html", "多条搜索索引记录")
	assertIssue(t, report.RecoverableIssues, ArchiveConsistencyStoreDatabase, "", "数据库统计总数")
	assertIssue(t, report.UnrecoverableIssues, ArchiveConsistencyStoreHTML, "bad.html", "无法解析")
	assertIssue(t, report.UnrecoverableIssues, ArchiveConsistencyStoreMeili, "lost.html", "HTML 文件不存在")
}

func TestArchiveConsistencyRepairCarriesUnrecoverableLossAndRefreshesDerivedStores(t *testing.T) {
	root := t.TempDir()
	writeArchiveHTML(t, root, "example.com", "page.html", "Page", "body")

	index := &fakeArchiveIndexStore{
		documents: []archiveIndexDocument{
			{ID: "stale", Domain: "missing.example", Filename: "gone.html"},
		},
		afterRebuildDocs: []archiveIndexDocument{
			{ID: "rebuilt", Domain: "example.com", Filename: "page.html"},
		},
		rebuildDocuments: 1,
	}
	stats := &fakeArchiveStatsStore{
		stats: &common.ArchiveStatsSnapshot{
			TotalFiles: 0,
		},
		refreshedStats: &common.ArchiveStatsSnapshot{
			TotalFiles: 1,
			Sources: []common.ArchiveStatItem{
				{Source: "example.com", FileCount: 1},
			},
		},
	}
	service := archiveConsistencyService{
		archiveRoot: root,
		index:       index,
		stats:       stats,
		now:         func() time.Time { return time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC) },
	}

	report, err := service.Repair(context.Background())
	if err != nil {
		t.Fatalf("Repair returned error: %v", err)
	}

	if !index.rebuildWasInvoked {
		t.Fatal("repair should rebuild the index")
	}
	if !stats.refreshWasInvoked {
		t.Fatal("repair should refresh archive stats")
	}
	if len(report.RecoverableIssues) != 0 {
		t.Fatalf("RecoverableIssues = %#v, want none", report.RecoverableIssues)
	}
	assertIssue(t, report.UnrecoverableIssues, ArchiveConsistencyStoreMeili, "gone.html", "无法自动恢复")
	if report.Consistent {
		t.Fatal("carried unrecoverable data loss should keep report inconsistent")
	}
	if report.IndexedDocuments != 1 || report.RefreshedStatSources != 1 {
		t.Fatalf("unexpected repair counters: %#v", report)
	}
	if len(report.Actions) != 2 {
		t.Fatalf("Actions length = %d, want 2", len(report.Actions))
	}
}

func TestScanArchiveHTMLFilesKeepsNestedFilenamesAndSkipsTemporary(t *testing.T) {
	root := t.TempDir()
	writeArchiveHTML(t, root, "example.com", "nested/page.htm", "Nested", "body")
	writeArchiveHTML(t, filepath.Join(root, "Temporary"), "", "upload.html", "Upload", "ignored")
	writeArchiveHTML(t, root, "", "root.html", "Root", "no domain")

	files, stats, issues, err := scanArchiveHTMLFiles(root)
	if err != nil {
		t.Fatalf("scanArchiveHTMLFiles returned error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("files = %#v, want one archive file", files)
	}
	if files[0].Domain != "example.com" || files[0].Filename != "nested/page.htm" {
		t.Fatalf("unexpected scanned file: %#v", files[0])
	}
	if len(stats) != 1 || stats[0].Source != "example.com" || stats[0].FileCount != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	assertIssue(t, issues, ArchiveConsistencyStoreHTML, "", "无法自动归属")
}

func TestScanArchiveHTMLFilesRejectsEmptyArchiveRoot(t *testing.T) {
	_, _, _, err := scanArchiveHTMLFiles(" ")
	if err == nil || !strings.Contains(err.Error(), "archive location is empty") {
		t.Fatalf("err = %v, want archive location error", err)
	}
}

func assertIssue(t *testing.T, issues []ArchiveConsistencyIssue, store string, filename string, messagePart string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Store == store && strings.Contains(issue.Filename, filename) && strings.Contains(issue.Message, messagePart) {
			return
		}
	}
	t.Fatalf("missing issue store=%s filename~=%s message~=%s in %#v", store, filename, messagePart, issues)
}

func writeArchiveHTML(t *testing.T, root string, domain string, filename string, title string, body string) {
	t.Helper()
	parts := []string{root}
	if domain != "" {
		parts = append(parts, domain)
	}
	parts = append(parts, filepath.FromSlash(filename))
	writeFile(t, filepath.Join(parts...), "<html><head><title>"+title+"</title></head><body>"+body+"</body></html>")
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}
