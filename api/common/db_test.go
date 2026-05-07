package common

import (
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestHashPasswordAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("secret-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" || hash == "secret-password" {
		t.Fatalf("unexpected hash: %q", hash)
	}
	if !checkPassword("secret-password", hash) {
		t.Fatal("checkPassword should accept the original password")
	}
	if checkPassword("wrong-password", hash) {
		t.Fatal("checkPassword should reject a wrong password")
	}
}

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

func TestUserDatabaseOperations(t *testing.T) {
	setupSQLiteDB(t)

	user, err := CreateUser("bob", "secret123")
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}
	if user.ID == 0 || user.Password == "secret123" {
		t.Fatalf("unexpected user: %#v", user)
	}
	if _, err := CreateUser("bob", "secret123"); err == nil {
		t.Fatal("duplicate user should return error")
	}

	admin, err := CreateUser("admin", "secret123")
	if err != nil || admin == nil {
		t.Fatalf("initial admin create = %#v err=%v", admin, err)
	}
	admin, err = CreateUser("admin", "secret123")
	if err != nil || admin != nil {
		t.Fatalf("duplicate admin create = %#v err=%v, want nil nil", admin, err)
	}

	loggedIn, err := LoginUser("bob", "secret123")
	if err != nil {
		t.Fatalf("LoginUser returned error: %v", err)
	}
	if loggedIn.ID != user.ID {
		t.Fatalf("loggedIn ID = %d, want %d", loggedIn.ID, user.ID)
	}
	if _, err := LoginUser("bob", "wrong"); err == nil {
		t.Fatal("wrong password should fail")
	}
	if _, err := LoginUser("missing", "secret123"); err == nil {
		t.Fatal("missing user should fail")
	}

	byID, err := GetUserByID(user.ID)
	if err != nil || byID.Username != "bob" {
		t.Fatalf("GetUserByID = %#v err=%v", byID, err)
	}
	byUsername, err := GetUserByUsername("bob")
	if err != nil || byUsername.ID != user.ID {
		t.Fatalf("GetUserByUsername = %#v err=%v", byUsername, err)
	}

	updated, err := UpdateUser(user.ID, map[string]interface{}{
		"username": "bobby",
		"password": "newsecret",
	})
	if err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if updated.Username != "bobby" || !checkPassword("newsecret", updated.Password) {
		t.Fatalf("unexpected updated user: %#v", updated)
	}
	if _, err := UpdateUser(9999, map[string]interface{}{"username": "none"}); err == nil {
		t.Fatal("updating missing user should fail")
	}

	users, total, err := GetAllUsers(1, 10)
	if err != nil {
		t.Fatalf("GetAllUsers returned error: %v", err)
	}
	if total != 2 || len(users) != 2 {
		t.Fatalf("users=%#v total=%d, want 2", users, total)
	}

	if err := DeleteUser(user.ID); err != nil {
		t.Fatalf("DeleteUser returned error: %v", err)
	}
	if _, err := GetUserByID(user.ID); err == nil {
		t.Fatal("deleted user should not be found")
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

func setupSQLiteDB(t *testing.T) {
	t.Helper()
	oldDB := db
	sqliteDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := sqliteDB.AutoMigrate(&User{}, &ArchiveTask{}, &ArchiveStat{}); err != nil {
		t.Fatalf("failed to migrate sqlite db: %v", err)
	}
	db = sqliteDB
	t.Cleanup(func() {
		db = oldDB
	})
}
