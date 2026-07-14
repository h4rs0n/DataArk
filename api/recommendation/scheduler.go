package recommendation

import (
	"DataArk/config"
	"context"
	"log"
	"strings"
	"time"
)

func StartRecommendationScheduler() func() {
	if !config.RECOMMENDATIONENABLED {
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
		date := recommendationDateForSettings(item, now)
		enqueued, err := EnqueueDailyRecommendation(ctx, item.UserID, date)
		if err != nil {
			log.Printf("recommendation scheduler enqueue failed for user %d: %v", item.UserID, err)
		}
		if enqueued {
			continue
		}
		if _, err := GenerateDailyRecommendations(ctx, item.UserID, date); err != nil {
			log.Printf("recommendation scheduler generation failed for user %d: %v", item.UserID, err)
		}
	}
}

func recommendationGenerationDue(settings RecommendationSettings, now time.Time) bool {
	location := recommendationLocation(settings)
	localNow := now.In(location)
	hour, minute, ok := parseRecommendationGenerationTime(settings.GenerationTime)
	if !ok {
		hour, minute = 7, 0
	}
	return localNow.Hour() > hour || (localNow.Hour() == hour && localNow.Minute() >= minute)
}

func recommendationDateForSettings(settings RecommendationSettings, now time.Time) string {
	return now.In(recommendationLocation(settings)).Format("2006-01-02")
}

func recommendationLocation(settings RecommendationSettings) *time.Location {
	name := firstNonEmpty(settings.Timezone, DefaultRecommendationSettings(settings.UserID).Timezone)
	location, err := time.LoadLocation(name)
	if err == nil {
		return location
	}
	fallback, err := time.LoadLocation(DefaultRecommendationSettings(settings.UserID).Timezone)
	if err == nil {
		return fallback
	}
	return time.UTC
}

func RecommendationDateForUser(userID uint, now time.Time) (string, error) {
	settings, err := GetRecommendationSettings(userID)
	if err != nil {
		return "", err
	}
	return recommendationDateForSettings(*settings, now), nil
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
