package discovery

import (
	appdatabase "DataArk/database"
	"os"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresDeferredMaterialMigrationAndAdmission(t *testing.T) {
	dsn := os.Getenv("DATAARK_MATERIAL_ADMISSION_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_MATERIAL_ADMISSION_POSTGRES_TEST_DSN to an empty disposable database")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var name string
	if err := database.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "dataark_v3_verify") {
		t.Fatal("refusing non-verification database")
	}
	if err := appdatabase.RunDatabaseMigrations(database); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := database.Table("discovery_candidates").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("requires empty disposable database", err)
	}
	if err := goose.DownTo(sqlDB, ".", 45); err != nil {
		t.Fatal(err)
	}
	// An existing historical placeholder must survive upgrading unchanged.
	for _, stmt := range []string{
		`INSERT INTO material(id,title) VALUES(900001,'Historical title')`,
		`INSERT INTO material_article_states(material_id) VALUES(900001)`,
		`INSERT INTO discovery_candidates(id,material_id,url,status) VALUES(900001,900001,'https://historical.example/post','new')`,
	} {
		if err := database.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	old := SetDB(database)
	t.Cleanup(func() { SetDB(old) })
	var historical DiscoveryCandidate
	if err := database.First(&historical, 900001).Error; err != nil || historical.Title != "Historical title" || historical.MaterialID != 900001 {
		t.Fatalf("upgrade lost history: %#v %v", historical, err)
	}
	verifyDeferredMaterial(t)
	if err := goose.DownTo(sqlDB, ".", 45); err == nil {
		t.Fatal("rollback silently discarded pending candidate metadata")
	}
	// Verify the failed downgrade was atomic, and the projection remains usable.
	if err := Candidates(database).Count(&count).Error; err != nil || count != 5 {
		t.Fatalf("failed downgrade damaged candidates: %d %v", count, err)
	}
}
