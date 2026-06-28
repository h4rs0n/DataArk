package common

import (
	"context"
	"log"
	"strings"
	"time"
)

func StartRecommendationScheduler() func() {
	if !RECOMMENDATIONENABLED {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		runDueRecommendationGeneration(context.Background(), time.Now())
		for {
			select {
			case <-ticker.C:
				runDueRecommendationGeneration(context.Background(), time.Now())
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

func runDueRecommendationGeneration(ctx context.Context, now time.Time) {
	if db == nil {
		return
	}
	var settings []RecommendationSettings
	if err := db.Where("enabled = ?", true).Find(&settings).Error; err != nil {
		log.Printf("recommendation scheduler settings query failed: %v", err)
		return
	}
	for _, item := range settings {
		if !recommendationGenerationDue(item, now) {
			continue
		}
		if _, err := GenerateDailyRecommendations(ctx, item.UserID, recommendationDateForSettings(item, now)); err != nil {
			log.Printf("recommendation scheduler generation failed for user %d: %v", item.UserID, err)
		}
	}
}

func recommendationGenerationDue(settings RecommendationSettings, now time.Time) bool {
	location, err := time.LoadLocation(firstNonEmpty(settings.Timezone, DefaultRecommendationSettings(settings.UserID).Timezone))
	if err != nil {
		location = time.Local
	}
	localNow := now.In(location)
	hour, minute, ok := parseRecommendationGenerationTime(settings.GenerationTime)
	if !ok {
		hour, minute = 7, 0
	}
	return localNow.Hour() > hour || (localNow.Hour() == hour && localNow.Minute() >= minute)
}

func recommendationDateForSettings(settings RecommendationSettings, now time.Time) string {
	location, err := time.LoadLocation(firstNonEmpty(settings.Timezone, DefaultRecommendationSettings(settings.UserID).Timezone))
	if err != nil {
		location = time.Local
	}
	return now.In(location).Format("2006-01-02")
}

func parseRecommendationGenerationTime(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	parsed, err := time.Parse("15:04", parts[0]+":"+parts[1])
	if err != nil {
		return 0, 0, false
	}
	return parsed.Hour(), parsed.Minute(), true
}
