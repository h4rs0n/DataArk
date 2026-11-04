package assessment

import (
	appdatabase "DataArk/database"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresBatchSchemaMigrationPreservesLegacyRequests(t *testing.T) {
	dsn := os.Getenv("DATAARK_BATCH_SCHEMA_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_BATCH_SCHEMA_POSTGRES_TEST_DSN to a disposable database")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	defer sqlDB.Close()
	var name string
	database.Raw("SELECT current_database()").Scan(&name)
	if !strings.HasPrefix(name, "dataark_v3_verify") {
		t.Fatal("refusing non-verification database")
	}
	// Production initializes River before application indexes. Initialize the
	// real schema, then return to 43 to seed an old-format active batch.
	if err := appdatabase.RunDatabaseMigrations(database); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(sqlDB, ".", 43); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"custom_id":"legacy","body":{"response_format":{"type":"json_object"}}}` + "\n")
	if err := database.Exec(`INSERT INTO assessment_batch_runs(id,active_key,status,base_url,model,policy_version,created_at) VALUES(1,1,'processing','https://batch.invalid','fixture','article-value-v4',now())`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO assessment_batches(run_id,uuid,model,phase,input_jsonl,next_check_at,created_at) VALUES(1,'legacy','fixture','prepared',?,now(),now())`, input).Error; err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(sqlDB, ".", 44); err != nil {
		t.Fatal(err)
	}
	var batch Batch
	if err := database.First(&batch).Error; err != nil {
		t.Fatal(err)
	}
	if batch.ResponseMode != ResponseObjectMode || !bytes.Equal(batch.InputJSONL, input) {
		t.Fatal("migration altered legacy request", batch.ResponseMode)
	}
	if err := database.Model(&batch).Update("response_mode", ResponseSchemaMode).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&batch).Update("response_mode", "unsupported").Error; err == nil {
		t.Fatal("invalid response mode accepted")
	}
	if err := goose.Down(sqlDB, "."); err == nil {
		t.Fatal("response mode migration rolled back an active run")
	}
}
