package bootstrap

import (
	"DataArk/archive"
	"DataArk/auth"
	"DataArk/database"
	"DataArk/discovery"
	"DataArk/recommendation"
	"fmt"
	"log"

	"gorm.io/gorm"
)

func InitDB() {
	database.InitDB()
	configureDomainDatabases(database.DB())
	if err := database.AutoMigrate(
		&auth.User{},
		&archive.ArchiveTask{},
		&archive.ArchiveStat{},
		&archive.ArchiveDocument{},
		&archive.SearchEvent{},
		&archive.ArchiveClickEvent{},
		&discovery.DiscoverySource{},
		&discovery.DiscoveryCandidate{},
		&discovery.DiscoveryCandidateFeedback{},
	); err != nil {
		log.Fatal("failed to migrate database", err)
	}
	if err := database.RunDatabaseMigrations(database.DB()); err != nil {
		log.Fatal("failed to run database migrations", err)
	}
	if err := migrateV3Compatibility(database.DB()); err != nil {
		log.Fatal("failed to migrate v3 compatibility data", err)
	}
	auth.CreateDefaultAdmin()
}

func migrateV3Compatibility(database *gorm.DB) error {
	if database == nil {
		return nil
	}
	if database.Dialector.Name() != "postgres" {
		models := append(discovery.V3Models(), recommendation.V3Models()...)
		if err := database.AutoMigrate(models...); err != nil {
			return fmt.Errorf("auto-migrate v3 models: %w", err)
		}
	}
	if err := auth.BackfillOwnerRole(database); err != nil {
		return fmt.Errorf("backfill owner role: %w", err)
	}
	if err := discovery.BackfillV3Compatibility(database); err != nil {
		return fmt.Errorf("backfill discovery v3: %w", err)
	}
	if err := recommendation.BackfillV3Compatibility(database); err != nil {
		return fmt.Errorf("backfill recommendation v3: %w", err)
	}
	return nil
}

func configureDomainDatabases(db *gorm.DB) {
	auth.SetDB(db)
	archive.SetDB(db)
	discovery.SetDB(db)
	recommendation.SetDB(db)
}
