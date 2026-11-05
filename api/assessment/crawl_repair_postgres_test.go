package assessment

import (
	"DataArk/config"
	appdatabase "DataArk/database"
	"DataArk/discovery"
	"DataArk/material"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresCrawlContentRepair(t *testing.T) {
	dsn := os.Getenv("DATAARK_CRAWL_REPAIR_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set DATAARK_CRAWL_REPAIR_POSTGRES_TEST_DSN to an empty disposable database")
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
	if err := goose.DownTo(sqlDB, ".", 48); err != nil {
		t.Fatal(err)
	}
	oldDB, oldDiscovery := SetDB(database), discovery.SetDB(database)
	oldModel, oldProvider := config.LLMCHATMODEL, config.LLMBATCHPROVIDER
	config.LLMCHATMODEL, config.LLMBATCHPROVIDER = "repair-fixture", ""
	t.Cleanup(func() {
		SetDB(oldDB)
		discovery.SetDB(oldDiscovery)
		config.LLMCHATMODEL, config.LLMBATCHPROVIDER = oldModel, oldProvider
	})
	if err := database.Create(&discovery.DiscoverySource{Name: "Repair fixture", URL: "https://repair.invalid/feed", Type: "rss", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	candidates := map[string]discovery.DiscoveryCandidate{}
	before := map[string]material.ArticleState{}
	for _, kind := range []string{"passed", "low_quality", "unchanged", "old_model", "unaccepted", "healthy", "superseded"} {
		candidate := createAssessmentCandidate(t, "Repair", "https://repair.invalid/"+kind, kind, strings.Repeat(kind+" evidence ", 80), now)
		quality := .9
		if kind == "low_quality" {
			quality = .1
		}
		assessor := ConfiguredArticleAssessor()
		row, err := persistArticleAssessment(candidate, assessor, ArticleAssessmentResult{Quality: quality, Depth: .7, Evergreen: .8, Confidence: .9, Reasons: []string{"Stored evidence"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := activateArticleAssessment(candidate, row, assessmentResultFromRow(row), ""); err != nil {
			t.Fatal(err)
		}
		if kind != "unaccepted" {
			if err := database.Model(&discovery.DiscoveryCandidate{}).Where("id=?", candidate.ID).Update("extracted_at", now).Error; err != nil {
				t.Fatal(err)
			}
		}
		if kind != "healthy" {
			if err := database.Model(&discovery.DiscoveryCandidate{}).Where("id=?", candidate.ID).Updates(map[string]any{"processing_state": "ineligible", "processing_error_type": "http_status", "updated_at": now.Add(time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Model(&material.ArticleState{}).Where("material_id=?", candidate.MaterialID).Updates(map[string]any{"eligibility_state": "ineligible", "eligibility_reasons": "http_status"}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if kind == "unchanged" {
			if err := database.Model(&material.ArticleState{}).Where("material_id=?", candidate.MaterialID).Updates(map[string]any{"eligibility_state": "unknown", "eligibility_reasons": "assessment_pending", "assessment_state": "pending"}).Error; err != nil {
				t.Fatal(err)
			}
		}
		if kind == "old_model" {
			if err := database.Model(&ArticleAssessment{}).Where("id=?", row.ID).Update("assessor_version", "retired-model").Error; err != nil {
				t.Fatal(err)
			}
		}
		var state material.ArticleState
		if err := database.First(&state, "material_id=?", candidate.MaterialID).Error; err != nil {
			t.Fatal(err)
		}
		candidates[kind], before[kind] = candidate, state
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
	var recorded int64
	if err := database.Table("material_crawl_repairs").Count(&recorded).Error; err != nil || recorded != 5 {
		t.Fatalf("repair evidence count=%d err=%v", recorded, err)
	}
	if err := database.Model(&material.ArticleState{}).Where("material_id=?", candidates["superseded"].MaterialID).Update("eligibility_reasons", "manual_decision").Error; err != nil {
		t.Fatal(err)
	}
	if err := RecoverCrawlContaminatedAssessments(context.Background()); err != nil {
		t.Fatal(err)
	}
	for kind, candidate := range candidates {
		var state material.ArticleState
		if err := database.First(&state, "material_id=?", candidate.MaterialID).Error; err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "passed", "unchanged":
			if state.EligibilityState != "eligible" || state.AssessmentState != "ready" {
				t.Fatalf("%s not restored: %+v", kind, state)
			}
		case "low_quality":
			if state.EligibilityState != "ineligible" || state.EligibilityReasons != "article_quality_below_threshold" {
				t.Fatalf("quality gate bypassed: %+v", state)
			}
		case "superseded":
			if state.EligibilityReasons != "manual_decision" {
				t.Fatal("overwrote subsequent user action")
			}
		default:
			if !reflect.DeepEqual(before[kind], state) {
				t.Fatalf("changed ambiguous or unaffected %s: %+v", kind, state)
			}
		}
	}
	var calls int64
	if err := database.Model(&LLMCall{}).Count(&calls).Error; err != nil || calls != 0 {
		t.Fatalf("repair made paid calls: %d %v", calls, err)
	}
	if err := RecoverCrawlContaminatedAssessments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(sqlDB, ".", 48); err != nil {
		t.Fatal(err)
	}
	var state material.ArticleState
	if err := database.First(&state, "material_id=?", candidates["passed"].MaterialID).Error; err != nil || state.EligibilityState != "eligible" {
		t.Fatal("downgrade restored contamination", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatal(err)
	}
}
