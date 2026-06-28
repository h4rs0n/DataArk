package common

import (
	"DataArk/migrations"
	"context"

	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
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
	if err := goose.Up(sqlDB, "."); err != nil {
		return err
	}
	migrator, err := rivermigrate.New(riverdatabasesql.New(sqlDB), nil)
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(context.Background(), rivermigrate.DirectionUp, nil)
	return err
}
