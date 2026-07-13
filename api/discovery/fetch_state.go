package discovery

import (
	"context"
	"errors"
	"net"
	"time"
)

var discoveryClock Clock = SystemClock{}

func finishDiscoveryFetch(source *DiscoverySource, result *DiscoveryFetchResult, fetched *FetchResult, startedAt time.Time, fetchErr error) error {
	if db == nil || source == nil || source.ID == 0 {
		return nil
	}
	now := discoveryClock.Now()
	status := "succeeded"
	failureCount := 0
	errorCategory := ""
	errorSummary := ""
	notModified := false
	httpStatus := 0
	finalURL := ""
	contentType := ""
	etag := ""
	lastModified := ""
	robotsStatus := ""
	updates := map[string]interface{}{
		"last_attempt_at": &now,
		"last_fetched_at": &now,
	}
	var scheduleDecision *SourceScheduleDecision
	if fetched != nil {
		notModified = fetched.NotModified
		httpStatus = fetched.StatusCode
		finalURL = fetched.FinalURL
		contentType = fetched.ContentType
		etag = fetched.ETag
		lastModified = fetched.LastModified
		robotsStatus = fetched.RobotsStatus
		if fetched.ETag != "" {
			updates["etag"] = fetched.ETag
		}
		if fetched.LastModified != "" {
			updates["last_modified"] = fetched.LastModified
		}
	}
	policy := ConfiguredSourceSchedulePolicy(discoveryClock)
	if fetchErr != nil {
		status = "failed"
		failureCount = source.FailureCount + 1
		errorCategory = discoveryFetchErrorCategory(fetchErr)
		errorSummary = fetchErr.Error()
		next := policy.NextFailure(source.ID, failureCount)
		scheduleDecision = &SourceScheduleDecision{Basis: "failure_backoff", Chosen: next.Sub(now), NextDueAt: next, Explanation: errorCategory}
		updates["failure_count"] = failureCount
		updates["next_fetch_at"] = &next
		updates["next_due_at"] = &next
		updates["backoff_until"] = &next
		updates["backoff_reason"] = errorCategory
		updates["last_error"] = errorSummary
	} else {
		decision := policy.DecideNextSuccess(*source, sourceSiteStatus(source.SiteID), result != nil && result.Stored > 0, loadDiscoverySiteOperationalStats(source.SiteID))
		next := decision.NextDueAt
		scheduleDecision = &decision
		updates["failure_count"] = 0
		updates["next_fetch_at"] = &next
		updates["next_due_at"] = &next
		updates["backoff_until"] = nil
		updates["backoff_reason"] = ""
		updates["last_error"] = ""
		updates["last_success_at"] = &now
	}
	if err := db.Model(source).Updates(updates).Error; err != nil {
		return err
	}
	if scheduleDecision != nil {
		if err := SaveDiscoverySourceScheduleDecision(*source, *scheduleDecision); err != nil {
			return err
		}
	}
	if source.SiteID != nil {
		siteUpdates := map[string]interface{}{"last_validated_at": &now}
		if errors.Is(fetchErr, ErrRobotsDisallowed) {
			siteUpdates["crawl_allowed"] = false
			siteUpdates["robots_status"] = "disallowed"
		} else if errors.Is(fetchErr, ErrRobotsUnavailable) {
			siteUpdates["crawl_allowed"] = false
			siteUpdates["robots_status"] = "unavailable"
		} else if fetchErr == nil {
			siteUpdates["crawl_allowed"] = true
			if robotsStatus != "" {
				siteUpdates["robots_status"] = robotsStatus
			}
		}
		if err := db.Model(&DiscoverySite{}).Where("id = ?", *source.SiteID).Updates(siteUpdates).Error; err != nil {
			return err
		}
	}
	for key, value := range updates {
		applySourceFetchUpdate(source, key, value)
	}
	finishedAt := now
	run := DiscoveryFetchRun{
		SiteID: source.SiteID, SourceID: source.ID, StartedAt: startedAt, FinishedAt: &finishedAt,
		Status: status, HTTPStatus: httpStatus, FinalURL: finalURL, ContentType: contentType,
		ETag: etag, LastModified: lastModified, RobotsStatus: robotsStatus, NotModified: notModified,
		NewCount: resultCount(result), DuplicateCount: duplicateResultCount(result), FailureCount: failureCount,
		ErrorCategory: errorCategory, ErrorSummary: errorSummary,
	}
	if err := db.Create(&run).Error; err != nil {
		return err
	}
	if source.SiteID != nil {
		_, err := RefreshDiscoverySiteOperationalStats(*source.SiteID)
		return err
	}
	return nil
}

func sourceSiteStatus(siteID *uint) string {
	if db == nil || siteID == nil {
		return ""
	}
	var site DiscoverySite
	if err := db.Select("status").First(&site, *siteID).Error; err != nil {
		return ""
	}
	return site.Status
}

func resultCount(result *DiscoveryFetchResult) int {
	if result == nil {
		return 0
	}
	return result.Stored
}

func duplicateResultCount(result *DiscoveryFetchResult) int {
	if result == nil || result.Discovered <= result.Stored {
		return 0
	}
	return result.Discovered - result.Stored
}

func discoveryFetchErrorCategory(err error) string {
	switch {
	case errors.Is(err, ErrRobotsDisallowed):
		return "robots_disallowed"
	case errors.Is(err, ErrRobotsUnavailable):
		return "robots_unavailable"
	case errors.Is(err, ErrUnsafeURLScheme), errors.Is(err, ErrUnsafeURLHost), errors.Is(err, ErrUnsafeURLPort), errors.Is(err, ErrUnsafeIPAddress):
		return "unsafe_url"
	case errors.Is(err, ErrHTTPFetchBodyTooLarge):
		return "body_too_large"
	case errors.Is(err, ErrHTTPFetchContentType):
		return "content_type"
	case errors.Is(err, ErrHTTPFetchStatus):
		return "http_status"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return "timeout"
		}
		return "network"
	}
	return "processing"
}

func applySourceFetchUpdate(source *DiscoverySource, key string, value interface{}) {
	switch key {
	case "failure_count":
		source.FailureCount, _ = value.(int)
	case "etag":
		source.ETag, _ = value.(string)
	case "last_modified":
		source.LastModified, _ = value.(string)
	case "last_error":
		source.LastError, _ = value.(string)
	case "backoff_reason":
		source.BackoffReason, _ = value.(string)
	case "next_due_at":
		source.NextDueAt, _ = value.(*time.Time)
	case "next_fetch_at":
		source.NextFetchAt, _ = value.(*time.Time)
	case "backoff_until":
		if value == nil {
			source.BackoffUntil = nil
		} else {
			source.BackoffUntil, _ = value.(*time.Time)
		}
	case "last_success_at":
		source.LastSuccessAt, _ = value.(*time.Time)
	case "last_attempt_at":
		source.LastAttemptAt, _ = value.(*time.Time)
	case "last_fetched_at":
		source.LastFetchedAt, _ = value.(*time.Time)
	}
}
