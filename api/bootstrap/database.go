package bootstrap

import (
	"DataArk/archive"
	"DataArk/assessment"
	"DataArk/assessmenteval"
	"DataArk/auth"
	"DataArk/database"
	"DataArk/discovery"
	"DataArk/recommendation"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
)

func InitDB() {
	database.InitDB()
	configureDomainDatabases(database.DB())
	// 生产 schema 只走 Goose + River，不再用 AutoMigrate 建表或改列。
	if err := database.RunDatabaseMigrations(database.DB()); err != nil {
		log.Fatal("failed to run database migrations", err)
	}
	if err := discovery.ReconcileMaterialIdentities(database.DB()); err != nil {
		log.Fatal("failed to reconcile material identities: ", err)
	}
	if err := archive.BackfillMaterialContent(); err != nil {
		log.Fatal("failed to backfill archive material: ", err)
	}
	if err := runV3CompatibilityOnce(database.DB()); err != nil {
		log.Fatal("failed to migrate v3 compatibility data", err)
	}
	if err := assessmenteval.RecoverInterruptedEvaluations(database.DB(), time.Now()); err != nil {
		log.Fatal("failed to recover article assessment evaluation workflow", err)
	}
	auth.CreateDefaultAdmin()
}

// v3CompatibilityCheckpoint 让一次性的 v3 数据回填只在首次启动跑一遍；生产库有
// 十万级候选，每次重启重跑会把端口绑定推迟一分多钟。
const v3CompatibilityCheckpoint = "v3-compatibility"

// runV3CompatibilityOnce 只在 Postgres 上查 checkpoint：SQLite 测试库没有 Goose 建的
// material_migration_checkpoints，而非 postgres 分支还要靠 AutoMigrate 建投影视图。
// migrateV3Compatibility 自身保持幂等，测试仍可直接反复调用。
func runV3CompatibilityOnce(database *gorm.DB) error {
	if database == nil || database.Dialector.Name() != "postgres" {
		return migrateV3Compatibility(database)
	}
	var completed int64
	if err := database.Table("material_migration_checkpoints").Where("name = ?", v3CompatibilityCheckpoint).Count(&completed).Error; err != nil {
		return err
	}
	if completed != 0 {
		return nil
	}
	if err := migrateV3Compatibility(database); err != nil {
		return err
	}
	return database.Exec("INSERT INTO material_migration_checkpoints(name) VALUES (?)", v3CompatibilityCheckpoint).Error
}

func migrateV3Compatibility(database *gorm.DB) error {
	if database == nil {
		return nil
	}
	if database.Dialector.Name() != "postgres" {
		// SQLite 测试无法跑 Postgres Goose；用 AutoMigrate 作为方言替身，不定义生产 schema。
		models := append(discovery.V3Models(), recommendation.V3Models()...)
		models = append(models, assessmenteval.WorkflowModels()...)
		models = append(models, assessment.V3Models()...)
		if err := database.AutoMigrate(models...); err != nil {
			return fmt.Errorf("auto-migrate v3 models: %w", err)
		}
		if err := discovery.MigrateMaterialTestSchema(database); err != nil {
			return err
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
	assessment.SetDB(db)
	recommendation.SetDB(db)
}
