package auth

import (
	"DataArk/database"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSQLiteDB(t *testing.T) {
	t.Helper()
	sqliteDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := sqliteDB.AutoMigrate(
		&User{},
	); err != nil {
		t.Fatalf("failed to migrate sqlite db: %v", err)
	}
	oldDatabase := database.SetDB(sqliteDB)
	oldAuth := SetDB(sqliteDB)
	t.Cleanup(func() {
		database.SetDB(oldDatabase)
		SetDB(oldAuth)
	})
}
