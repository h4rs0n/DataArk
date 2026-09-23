package archive

import (
	"DataArk/database"
	"DataArk/material"
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
		&ArchiveTask{},
		&ArchiveStat{},
		&ArchiveDocument{},
		&SearchEvent{},
		&ArchiveClickEvent{},
	); err != nil {
		t.Fatalf("failed to migrate sqlite db: %v", err)
	}
	if err := sqliteDB.AutoMigrate(material.Models()...); err != nil {
		t.Fatal(err)
	}
	oldDatabase := database.SetDB(sqliteDB)
	oldArchive := SetDB(sqliteDB)
	t.Cleanup(func() {
		database.SetDB(oldDatabase)
		SetDB(oldArchive)
	})
}
