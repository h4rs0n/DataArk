package archive

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestBuildArchiveStatsSnapshotSkipsNegativeCounts(t *testing.T) {
	snapshot := buildArchiveStatsSnapshot([]ArchiveStat{
		{Source: "a.example", FileCount: 2},
		{Source: "bad.example", FileCount: -1},
		{Source: "b.example", FileCount: 3},
	})

	if snapshot.TotalFiles != 5 {
		t.Fatalf("TotalFiles = %d, want 5", snapshot.TotalFiles)
	}
	if len(snapshot.Sources) != 2 {
		t.Fatalf("Sources = %#v, want 2 entries", snapshot.Sources)
	}
}

func TestArchiveTaskDatabaseOperations(t *testing.T) {
	setupSQLiteDB(t)
	now := time.Now()
	task := &ArchiveTask{
		ID:        "task-1",
		URL:       "https://example.com",
		Domain:    "example.com",
		Status:    "pending",
		StartedAt: &now,
	}
	if err := CreateArchiveTask(task); err != nil {
		t.Fatalf("CreateArchiveTask returned error: %v", err)
	}
	task.Status = "success"
	task.FileName = "page.html"
	if err := SaveArchiveTask(task); err != nil {
		t.Fatalf("SaveArchiveTask returned error: %v", err)
	}

	loaded, err := GetArchiveTaskByID("task-1")
	if err != nil {
		t.Fatalf("GetArchiveTaskByID returned error: %v", err)
	}
	if loaded.Status != "success" || loaded.FileName != "page.html" {
		t.Fatalf("loaded task = %#v", loaded)
	}

	latest, err := GetLatestArchiveTaskByURL("https://example.com")
	if err != nil || latest.ID != "task-1" {
		t.Fatalf("GetLatestArchiveTaskByURL = %#v err=%v", latest, err)
	}
	if _, err := FindActiveArchiveTaskByURL("https://example.com"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("FindActiveArchiveTaskByURL err = %v, want record not found", err)
	}
	task2 := &ArchiveTask{ID: "task-2", URL: "https://example.com", Domain: "example.com", Status: "running"}
	if err := CreateArchiveTask(task2); err != nil {
		t.Fatal(err)
	}
	active, err := FindActiveArchiveTaskByURL("https://example.com")
	if err != nil || active.ID != "task-2" {
		t.Fatalf("FindActiveArchiveTaskByURL = %#v err=%v", active, err)
	}
	tasks, err := ListArchiveTasksByStatuses([]string{"running"})
	if err != nil || len(tasks) != 1 || tasks[0].ID != "task-2" {
		t.Fatalf("ListArchiveTasksByStatuses = %#v err=%v", tasks, err)
	}
}

func TestArchiveStatsDatabaseOperations(t *testing.T) {
	setupSQLiteDB(t)
	replaced, err := ReplaceArchiveStats([]ArchiveStat{
		{Source: "b.example", FileCount: 2},
		{Source: "a.example", FileCount: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceArchiveStats returned error: %v", err)
	}
	if replaced.TotalFiles != 3 {
		t.Fatalf("replaced total = %d, want 3", replaced.TotalFiles)
	}

	stats, err := GetArchiveStats()
	if err != nil {
		t.Fatalf("GetArchiveStats returned error: %v", err)
	}
	if stats.TotalFiles != 3 || stats.Sources[0].Source != "a.example" {
		t.Fatalf("stats = %#v", stats)
	}

	if err := IncrementArchiveStat("a.example", 2); err != nil {
		t.Fatalf("IncrementArchiveStat existing returned error: %v", err)
	}
	if err := IncrementArchiveStat("c.example", 4); err != nil {
		t.Fatalf("IncrementArchiveStat new returned error: %v", err)
	}
	if err := IncrementArchiveStat(" ", 2); err != nil {
		t.Fatalf("IncrementArchiveStat empty should be ignored: %v", err)
	}

	if err := DecrementArchiveStat("a.example", 1); err != nil {
		t.Fatalf("DecrementArchiveStat returned error: %v", err)
	}
	if err := DecrementArchiveStat("b.example", 5); err != nil {
		t.Fatalf("DecrementArchiveStat delete returned error: %v", err)
	}
	if err := DecrementArchiveStat("missing.example", 1); err != nil {
		t.Fatalf("DecrementArchiveStat missing should be ignored: %v", err)
	}
	if err := DecrementArchiveStat("", 1); err != nil {
		t.Fatalf("DecrementArchiveStat empty should be ignored: %v", err)
	}

	stats, err = GetArchiveStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalFiles != 6 {
		t.Fatalf("stats after updates = %#v, want total 6", stats)
	}

	replaced, err = ReplaceArchiveStats(nil)
	if err != nil {
		t.Fatalf("ReplaceArchiveStats nil returned error: %v", err)
	}
	if replaced.TotalFiles != 0 {
		t.Fatalf("empty replacement total = %d, want 0", replaced.TotalFiles)
	}
}
