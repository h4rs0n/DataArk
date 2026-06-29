package bootstrap

import (
	"DataArk/archive"
	"DataArk/auth"
	"DataArk/database"
	"DataArk/discovery"
	"DataArk/recommendation"
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
	auth.CreateDefaultAdmin()
}

func configureDomainDatabases(db *gorm.DB) {
	auth.SetDB(db)
	archive.SetDB(db)
	discovery.SetDB(db)
	recommendation.SetDB(db)
}
