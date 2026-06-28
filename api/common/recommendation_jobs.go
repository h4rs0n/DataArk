package common

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

const RecommendationGenerateDailyJobKind = "recommendation_generate_daily"

var recommendationRiverClient *river.Client[*sql.Tx]

type GenerateDailyRecommendationArgs struct {
	UserID uint   `json:"user_id"`
	Date   string `json:"date"`
}

func (GenerateDailyRecommendationArgs) Kind() string { return RecommendationGenerateDailyJobKind }

func (args GenerateDailyRecommendationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 24 * time.Hour,
		},
	}
}

type GenerateDailyRecommendationWorker struct {
	river.WorkerDefaults[GenerateDailyRecommendationArgs]
}

func (worker *GenerateDailyRecommendationWorker) Work(ctx context.Context, job *river.Job[GenerateDailyRecommendationArgs]) error {
	_, err := GenerateDailyRecommendationsWithReranker(ctx, job.Args.UserID, job.Args.Date, ConfiguredRecommendationReranker())
	return err
}

func StartRecommendationJobQueue(ctx context.Context) (func(), error) {
	if db == nil || db.Dialector.Name() != "postgres" {
		return func() {}, nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return func() {}, err
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &GenerateDailyRecommendationWorker{})
	client, err := river.NewClient(riverdatabasesql.New(sqlDB), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 2},
		},
		Workers: workers,
	})
	if err != nil {
		return func() {}, err
	}
	if err := client.Start(ctx); err != nil {
		return func() {}, err
	}
	recommendationRiverClient = client
	return func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Stop(stopCtx); err != nil {
			log.Printf("recommendation river queue stop failed: %v", err)
		}
		if recommendationRiverClient == client {
			recommendationRiverClient = nil
		}
	}, nil
}

func EnqueueDailyRecommendation(ctx context.Context, userID uint, date string) (bool, error) {
	if recommendationRiverClient == nil {
		return false, nil
	}
	_, err := recommendationRiverClient.Insert(ctx, GenerateDailyRecommendationArgs{
		UserID: userID,
		Date:   normalizeRecommendationDate(date),
	}, nil)
	if err != nil {
		return false, err
	}
	return true, nil
}
