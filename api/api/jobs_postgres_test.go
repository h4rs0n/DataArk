package api

import (
	"DataArk/articlevalue"
	"DataArk/assessment"
	"DataArk/config"
	appdatabase "DataArk/database"
	"DataArk/discovery"
	"DataArk/jobqueue"
	"DataArk/material"
	"DataArk/recommendation"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresApplicationQueueSurvivesBatchRecoveryFailure(t *testing.T) {
	dsn := os.Getenv("DATAARK_QUEUE_ISOLATION_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_QUEUE_ISOLATION_POSTGRES_TEST_DSN to an empty disposable database")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	defer sqlDB.Close()
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
	oldDatabase, oldDiscovery := appdatabase.SetDB(database), discovery.SetDB(database)
	oldAssessment, oldRecommendation := assessment.SetDB(database), recommendation.SetDB(database)
	oldKey, oldProvider := config.LLMBATCHAPIKEY, config.LLMBATCHPROVIDER
	config.LLMBATCHAPIKEY, config.LLMBATCHPROVIDER = "", ""
	t.Cleanup(func() {
		appdatabase.SetDB(oldDatabase)
		discovery.SetDB(oldDiscovery)
		assessment.SetDB(oldAssessment)
		recommendation.SetDB(oldRecommendation)
		config.LLMBATCHAPIKEY, config.LLMBATCHPROVIDER = oldKey, oldProvider
	})
	one := 1
	run := assessment.BatchRun{ActiveKey: &one, AdmissionEnabled: true, Status: "processing", BaseURL: "https://original.invalid/v1", Model: "fixture", PolicyVersion: "fixture", CreatedAt: time.Now()}
	if err := database.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	// Realtime may reuse a stored result without accessing any external model.
	core, err := material.EnsureURL(database, "https://queue.invalid/realtime", "Retained article", "")
	if err != nil {
		t.Fatal(err)
	}
	version, err := material.AppendText(database, core, strings.Repeat("Retained evidence. ", 40), 80, "archive_html", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&material.ArticleState{}).Where("material_id=?", core.ID).Update("content_version", version.Version).Error; err != nil {
		t.Fatal(err)
	}
	assessorName, assessorVersion := config.ArticleAssessmentIdentity()
	stored := assessment.ArticleAssessment{MaterialID: core.ID, ContentVersion: version.Version, Assessor: assessorName, AssessorVersion: assessorVersion, PolicyVersion: articlevalue.PolicyVersion, OverallQuality: .9, Depth: .8, EvergreenValue: .7, Confidence: .9, Reasons: `["Stored evidence"]`, CreatedAt: time.Now()}
	if err := database.Create(&stored).Error; err != nil {
		t.Fatal(err)
	}
	// This persisted tick is runnable as soon as River starts. It must be guarded.
	if err := database.Exec(`INSERT INTO river_job(kind,args,queue,state,scheduled_at,max_attempts,priority)
 VALUES('assessment_batch_tick','{"at":1}','assessment_batch','available',CURRENT_TIMESTAMP,25,1)`).Error; err != nil {
		t.Fatal(err)
	}
	stop, err := startApplicationJobQueue(context.Background())
	if err != nil {
		t.Fatalf("Batch failure stopped shared runtime: %v", err)
	}
	defer stop()
	queue, ok := jobqueue.Default()
	if !ok {
		t.Fatal("shared queue unavailable")
	}
	if err := queue.EnqueueFetchSource(context.Background(), 999999); err != nil {
		t.Fatal(err)
	}
	controller, ok := jobqueue.CrawlControl()
	if !ok {
		t.Fatal("crawl controls unavailable")
	}
	if _, err := controller.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := queue.EnqueueAssessArticle(context.Background(), core.ID, "1"); err != nil {
		t.Fatal(err)
	}
	assessmentController, ok := jobqueue.AssessmentControl()
	if !ok {
		t.Fatal("realtime assessment controls unavailable")
	}
	if _, err := assessmentController.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var completed int64
		if err := database.Table("river_job").Where("kind=? AND state='completed'", jobqueue.FetchSourceJobKind).Count(&completed).Error; err != nil {
			t.Fatal(err)
		}
		var current material.ArticleState
		if err := database.First(&current, "material_id=?", core.ID).Error; err != nil {
			t.Fatal(err)
		}
		if completed > 0 && current.AssessmentState == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("unrelated crawl or realtime assessment worker did not execute")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err := database.First(&run, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.ActiveKey == nil || run.AdmissionEnabled || run.Status != "processing" {
		t.Fatalf("Batch durable state lost: %+v", run)
	}
	if err := assessment.ProcessBatchTick(context.Background()); err == nil {
		t.Fatal("persisted tick bypassed Batch recovery guard")
	}
}
