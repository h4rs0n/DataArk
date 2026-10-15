// assessment-batch-reset runs migrations and resets an explicitly selected
// Batch run without bootstrapping the API, workers, or a provider client.
package main

import (
	"DataArk/assessment"
	"DataArk/database"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	runID := flag.Uint("run-id", 0, "explicit Batch run to return to River")
	apply := flag.Bool("apply", false, "apply reset; default is dry-run")
	migrate := flag.Bool("migrate", false, "apply application migrations before reset")
	flag.Parse()
	if *runID == 0 || os.Getenv("DATAARK_RESET_DSN") == "" {
		fmt.Fprintln(os.Stderr, "run-id and DATAARK_RESET_DSN are required")
		os.Exit(1)
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("DATAARK_RESET_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "database connection failed")
		os.Exit(1)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if *migrate {
		if err := database.RunDatabaseMigrations(db); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	assessment.SetDB(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := assessment.ResetBatchRun(ctx, uint(*runID), !*apply)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
