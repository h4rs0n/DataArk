package api

import (
	"DataArk/auth"
	"DataArk/recommendation"
	"net/http"
	"testing"
	"time"
)

func TestDailyDigestM14TodayUsesAuthenticatedUsersLocalDate(t *testing.T) {
	oldDate := recommendationDateForUser
	oldNow := recommendationNow
	oldSnapshot := getRecommendationDaySnapshot
	t.Cleanup(func() {
		recommendationDateForUser = oldDate
		recommendationNow = oldNow
		getRecommendationDaySnapshot = oldSnapshot
	})
	now := time.Date(2026, 1, 1, 16, 30, 0, 0, time.UTC)
	recommendationNow = func() time.Time { return now }
	recommendationDateForUser = func(userID uint, got time.Time) (string, error) {
		if userID != 901 || !got.Equal(now) {
			t.Fatalf("date identity = %d/%s", userID, got)
		}
		return "2026-01-02", nil
	}
	getRecommendationDaySnapshot = func(userID uint, date string) (*recommendation.RecommendationDaySnapshot, error) {
		if userID != 901 || date != "2026-01-02" {
			t.Fatalf("snapshot identity = %d/%s", userID, date)
		}
		return &recommendation.RecommendationDaySnapshot{Day: &recommendation.RecommendationDay{UserID: userID, RecommendationDate: date, Status: recommendation.RecommendationDayStatusPublished}}, nil
	}
	response := performUserControllerRequest(http.MethodGet, "/recommendations/today", nil, &auth.User{ID: 901}, GetRecommendationToday)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}
