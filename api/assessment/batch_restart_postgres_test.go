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

func TestPostgresBatchAdmissionMigration(t *testing.T) {
	dsn := os.Getenv("DATAARK_BATCH_ADMISSION_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_BATCH_ADMISSION_POSTGRES_TEST_DSN to a disposable database")
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
	if err := appdatabase.RunDatabaseMigrations(database); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(sqlDB, ".", 44); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO assessment_batch_runs(id,active_key,status,base_url,model,policy_version,high_water,cursor,created_at) VALUES(1,1,'preparing','https://fixture.invalid','fixture','article-value-v4',100,50,now())`).Error; err != nil {
		t.Fatal(err)
	}
	input := []byte("persisted original request")
	if err := database.Exec(`INSERT INTO assessment_batches(run_id,uuid,model,phase,input_jsonl,next_check_at,created_at) VALUES(1,'admission-legacy','fixture','prepared',?,now(),now())`, input).Error; err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	var run BatchRun
	if err := database.First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if run.AdmissionEnabled || run.HighWater != 100 || run.Cursor != 50 || run.Status != "preparing" {
		t.Fatal("migration changed run", run)
	}
	var b Batch
	if err := database.First(&b).Error; err != nil || !bytes.Equal(b.InputJSONL, input) || b.Phase != "prepared" {
		t.Fatal("migration changed batch", b, err)
	}
	if err := goose.Down(sqlDB, "."); err == nil {
		t.Fatal("active admission gate can be rolled back")
	}
	if err := database.Model(&run).Updates(map[string]any{"active_key": nil, "status": "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := goose.Down(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
}
