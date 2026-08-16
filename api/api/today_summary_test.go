package api

import (
	"DataArk/auth"
	"DataArk/recommendation"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRecommendationTodaySummaryUsesAuthenticatedUsersLocalDate(t *testing.T) {
	oldDate := recommendationDateForUser
	oldNow := recommendationNow
	oldSummary := getRecommendationDaySummary
	t.Cleanup(func() {
		recommendationDateForUser = oldDate
		recommendationNow = oldNow
		getRecommendationDaySummary = oldSummary
	})
	now := time.Date(2026, 6, 1, 16, 30, 0, 0, time.UTC)
	recommendationNow = func() time.Time { return now }
	recommendationDateForUser = func(userID uint, got time.Time) (string, error) {
		if userID != 905 || !got.Equal(now) {
			t.Fatalf("date identity = %d/%s", userID, got)
		}
		return "2026-06-02", nil
	}
	getRecommendationDaySummary = func(_ context.Context, userID uint, date string) (*recommendation.RecommendationDaySummary, error) {
		if userID != 905 || date != "2026-06-02" {
			t.Fatalf("summary identity = %d/%s", userID, date)
		}
		return &recommendation.RecommendationDaySummary{Date: date, Available: true, Overview: "今日总结。", Highlights: []string{}, Topics: []string{}}, nil
	}
	response := performUserControllerRequest(http.MethodGet, "/recommendations/today/summary", nil, &auth.User{ID: 905}, GetRecommendationTodaySummary)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"Status":"1"`) || !strings.Contains(response.Body.String(), "今日总结。") {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestRecommendationTodaySummaryPropagatesServiceErrors(t *testing.T) {
	oldSummary := getRecommendationDaySummary
	t.Cleanup(func() { getRecommendationDaySummary = oldSummary })
	getRecommendationDaySummary = func(_ context.Context, _ uint, _ string) (*recommendation.RecommendationDaySummary, error) {
		return nil, errors.New("summary broken")
	}
	response := performUserControllerRequest(http.MethodGet, "/recommendations/today/summary", nil, &auth.User{ID: 905}, GetRecommendationTodaySummary)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"Status":"0"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}
