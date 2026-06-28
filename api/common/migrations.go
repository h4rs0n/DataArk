package common

import (
	"DataArk/migrations"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

func RunDatabaseMigrations(database *gorm.DB) error {
	if database == nil || database.Dialector.Name() != "postgres" {
		return nil
	}

	sqlDB, err := database.DB()
	if err != nil {
		return err
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(sqlDB, ".")
}
