// Reimport completed Batch outputs without starting workers or inference.
package main

import (
	"DataArk/assessment"
	"DataArk/config"
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
	runID := flag.Uint("run-id", 0, "explicit Batch run to reimport")
	apply := flag.Bool("apply", false, "persist compatible results; default is preview")
	stdin := flag.Bool("config-stdin", false, "read private configuration from stdin instead of environment")
	flag.Parse()
	settings := struct {
		DSN              string  `json:"dsn"`
		BaseURL          string  `json:"baseUrl"`
		APIKey           string  `json:"apiKey"`
		QualityThreshold float64 `json:"qualityThreshold"`
	}{DSN: os.Getenv("DATAARK_REIMPORT_DSN"), BaseURL: os.Getenv("DATAARK_REIMPORT_BASE_URL"), APIKey: os.Getenv("DATAARK_REIMPORT_API_KEY")}
	if *stdin {
		if json.NewDecoder(os.Stdin).Decode(&settings) != nil {
			fail("invalid private configuration")
		}
	}
	if *runID == 0 || settings.DSN == "" || settings.BaseURL == "" || settings.APIKey == "" {
		fail("run-id, database DSN and original Batch credentials are required")
	}
	config.LLMBATCHBASEURL, config.LLMBATCHAPIKEY = settings.BaseURL, settings.APIKey
	config.DISCOVERYARTICLEQUALITYTHRESHOLD = settings.QualityThreshold
	db, err := gorm.Open(postgres.Open(settings.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fail("database connection failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		fail("database connection unavailable")
	}
	defer sqlDB.Close()
	assessment.SetDB(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	stats, err := assessment.ReimportBatchRun(ctx, *runID, !*apply)
	_ = json.NewEncoder(os.Stdout).Encode(stats)
	if err != nil {
		fail("Batch reimport stopped: " + err.Error())
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
