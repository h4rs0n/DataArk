package discovery

import (
	"DataArk/archive"
	"DataArk/config"
	"DataArk/discovery/articlerules"
	"DataArk/jobqueue"
	"DataArk/observability"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DiscoveryDedupePending     = "pending"
	DiscoveryAssessmentPending = "pending"

	processingErrorHTTPStatus       = "http_status"
	processingErrorNotArticle       = "not_article"
	processingErrorMissingTitle     = "missing_title"
	processingErrorBodyTooShort     = "body_too_short"
	processingErrorLanguageUnknown  = "language_unknown"
	processingErrorExtractionFailed = "extraction_failed"
)

func RunProcessCandidateJob(ctx context.Context, candidateID uint, contentVersion string) error {
	err := ProcessCandidate(ctx, candidateID, contentVersion)
	event := observability.Event{Name: "candidate_processed", CandidateID: candidateID, Status: DiscoveryProcessingReady}
	if err != nil {
		event.Status, event.ErrorType = DiscoveryProcessingFailed, "processing"
	}
	observability.Log(event)
	return err
}

// ProcessCandidate safely fetches and extracts one logical article. The version
// argument is an optimistic guard: stale queued work becomes a no-op after a
// newer body has already been committed. LLM assessment is a separate job.
func ProcessCandidate(ctx context.Context, candidateID uint, expectedVersion string) error {
	if db == nil || candidateID == 0 {
		return gorm.ErrRecordNotFound
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return err
	}
	expected, err := parseExpectedContentVersion(expectedVersion)
	if err != nil {
		return err
	}
	if expected != candidate.ContentVersion {
		return nil
	}
	// 抽取完成但去重未完成：只补去重并交接评估，避免重复抓取。
	// 已 pending 的评估不在这里短路，否则正文更新无法再进入抽取。
	if candidate.ProcessingState == DiscoveryProcessingReady && candidate.DedupeState == DiscoveryDedupePending && candidate.ContentHash != "" {
		if err := ResolveCandidateDuplicates(ctx, candidate.ID); err != nil {
			return err
		}
		return enqueueCandidateForAssessment(ctx, candidate.ID)
	}
	if reason, rejected := articlerules.RejectURL(candidate.URL, candidate.Title); rejected {
		return finishCandidateIneligible(&candidate, processingErrorNotArticle, reason, discoveryClock.Now())
	}
	if err := ensureDiscoveryURLNotBlacklisted(ctx, candidate.URL); err != nil {
		if errors.Is(err, ErrDiscoveryDomainBlacklisted) {
			updates := candidateFailureUpdates(DiscoveryProcessingDomainBlocked, DiscoveryEligibilityUnknown, processingErrorDomainBlacklist, err.Error(), discoveryClock.Now())
			updates["eligibility_reasons"] = processingErrorDomainBlacklist
			updates["next_processing_at"] = nil
			if updateErr := db.Model(&candidate).Updates(updates).Error; updateErr != nil {
				return updateErr
			}
		}
		return err
	}

	now := discoveryClock.Now()
	attempt := candidate.ProcessingAttempts + 1
	if err := db.Model(&candidate).Updates(map[string]interface{}{
		"processing_state":    DiscoveryProcessingFetching,
		"processing_attempts": attempt,
		"next_processing_at":  nil,
		"updated_at":          now,
	}).Error; err != nil {
		return err
	}
	candidate.ProcessingAttempts = attempt

	response, err := fetchDiscoveryRequest(ctx, FetchRequest{URL: candidate.URL, Kind: FetchKindArticle})
	if err != nil {
		return recordCandidateFetchFailure(&candidate, err, now)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		statusError := withHTTPStatusDiagnostic(fmt.Errorf("%w: article returned status %d", ErrHTTPFetchStatus, response.StatusCode), response, candidate.URL)
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
			return recordCandidateFetchFailure(&candidate, statusError, now)
		}
		return finishCandidateIneligible(&candidate, processingErrorHTTPStatus, statusError.Error(), now)
	}

	finalURL := strings.TrimSpace(response.FinalURL)
	if finalURL == "" {
		finalURL = candidate.URL
	}
	article, err := ExtractArticle(finalURL, response.Body)
	if err != nil {
		return finishCandidateReview(&candidate, processingErrorExtractionFailed, err.Error(), now)
	}
	if reason, category, review := articleEligibilityFailure(candidate.URL, article); category != "" {
		if review {
			return finishCandidateReview(&candidate, category, reason, now)
		}
		return finishCandidateIneligible(&candidate, category, reason, now)
	}
	if err := commitExtractedArticle(candidate.ID, expected, finalURL, response.FetchedAt, article, now); err != nil {
		return err
	}
	if err := ResolveCandidateDuplicates(ctx, candidate.ID); err != nil {
		return err
	}
	return enqueueCandidateForAssessment(ctx, candidate.ID)
}

// enqueueCandidateForAssessment 把已抽取的代表文章标为 pending 并交给评估队列。
// 没有作业队列时停在 pending，由 assessment 包在有队列或测试中显式调用 AssessCandidate。
func enqueueCandidateForAssessment(ctx context.Context, candidateID uint) error {
	if db == nil || candidateID == 0 {
		return gorm.ErrRecordNotFound
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return err
	}
	if candidate.ProcessingState != DiscoveryProcessingReady {
		return nil
	}
	if err := db.Model(&candidate).Updates(map[string]interface{}{
		"assessment_state": DiscoveryAssessmentPending, "updated_at": discoveryClock.Now(),
	}).Error; err != nil {
		return err
	}
	queue, available := jobqueue.Default()
	if available && queue != nil {
		return queue.EnqueueAssessArticle(ctx, candidate.ID, candidateContentVersion(candidate))
	}
	return nil
}

func parseExpectedContentVersion(value string) (uint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid candidate content version %q: %w", value, err)
	}
	return uint(parsed), nil
}

func articleEligibilityFailure(rawURL string, article *ExtractedArticle) (string, string, bool) {
	if article == nil {
		return "article extraction returned no result", processingErrorExtractionFailed, true
	}
	minimumCharacters := config.DISCOVERYARTICLEMINCHARS
	if minimumCharacters <= 0 {
		minimumCharacters = 120
	}
	return articlerules.RejectPage(articlerules.Page{
		URL: rawURL, Title: article.Title, Text: article.Text, Language: article.Language,
		IsArticle: article.IsArticle, HasPasswordInput: article.HasPasswordInput,
	}, minimumCharacters)
}

func recordCandidateFetchFailure(candidate *DiscoveryCandidate, fetchErr error, now time.Time) error {
	category := discoveryFetchErrorCategory(fetchErr)
	if isPermanentCandidateFetchError(fetchErr) {
		return finishCandidateIneligible(candidate, category, fetchErr.Error(), now)
	}
	maximumAttempts := config.DISCOVERYPROCESSINGMAXATTEMPTS
	if maximumAttempts <= 0 {
		maximumAttempts = 5
	}
	if int(candidate.ProcessingAttempts) >= maximumAttempts {
		updates := candidateFailureUpdates(DiscoveryProcessingFailed, DiscoveryEligibilityReview, category, fetchErr.Error(), now)
		updates["next_processing_at"] = nil
		return db.Model(candidate).Updates(updates).Error
	}
	next := now.Add(candidateRetryDelay(candidate.ProcessingAttempts))
	updates := candidateFailureUpdates(DiscoveryProcessingFetchPending, DiscoveryEligibilityUnknown, category, fetchErr.Error(), now)
	updates["next_processing_at"] = &next
	if err := db.Model(candidate).Updates(updates).Error; err != nil {
		return err
	}
	return fetchErr
}

func isPermanentCandidateFetchError(err error) bool {
	return errors.Is(err, ErrDiscoveryDomainBlacklisted) || errors.Is(err, ErrRobotsDisallowed) ||
		errors.Is(err, ErrUnsafeURLScheme) || errors.Is(err, ErrUnsafeURLHost) ||
		errors.Is(err, ErrUnsafeURLPort) || errors.Is(err, ErrUnsafeIPAddress) ||
		errors.Is(err, ErrHTTPFetchBodyTooLarge) || errors.Is(err, ErrHTTPFetchContentType)
}

func candidateRetryDelay(attempt uint) time.Duration {
	delay := 5 * time.Minute
	for current := uint(1); current < attempt && delay < 24*time.Hour; current++ {
		delay *= 2
	}
	if delay > 24*time.Hour {
		return 24 * time.Hour
	}
	return delay
}

func finishCandidateIneligible(candidate *DiscoveryCandidate, category string, summary string, now time.Time) error {
	updates := candidateFailureUpdates(DiscoveryProcessingIneligible, DiscoveryEligibilityIneligible, category, summary, now)
	updates["next_processing_at"] = nil
	updates["eligibility_reasons"] = category
	return db.Model(candidate).Updates(updates).Error
}

func finishCandidateReview(candidate *DiscoveryCandidate, category string, summary string, now time.Time) error {
	updates := candidateFailureUpdates(DiscoveryProcessingReview, DiscoveryEligibilityReview, category, summary, now)
	updates["next_processing_at"] = nil
	updates["eligibility_reasons"] = category
	return db.Model(candidate).Updates(updates).Error
}

func candidateFailureUpdates(processingState string, eligibilityState string, category string, summary string, now time.Time) map[string]interface{} {
	return map[string]interface{}{
		"processing_state":      processingState,
		"eligibility_state":     eligibilityState,
		"processing_error_type": strings.TrimSpace(category),
		"processing_error":      compactProcessingError(summary),
		"updated_at":            now,
	}
}

func compactProcessingError(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return value
}

func commitExtractedArticle(candidateID uint, expectedVersion uint, finalURL string, fetchedAt time.Time, article *ExtractedArticle, now time.Time) error {
	if fetchedAt.IsZero() {
		fetchedAt = now
	}
	contentHash := ContentHash(article.Text)
	canonicalURL := resolveArticleCanonicalURL(finalURL, article.CanonicalURL)
	return db.Transaction(func(tx *gorm.DB) error {
		var current DiscoveryCandidate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, candidateID).Error; err != nil {
			return err
		}
		if current.ContentVersion != expectedVersion {
			return nil
		}
		contentVersion := current.ContentVersion
		bodyChanged := current.ContentHash != contentHash
		if bodyChanged {
			contentVersion++
			version := DiscoveryArticleContentVersion{
				CandidateID: candidateID, ContentVersion: contentVersion, ContentHash: contentHash,
				FinalURL: finalURL, CanonicalURL: canonicalURL, Title: article.Title,
				Summary: archive.BuildSummary(article.Description, 260), Author: article.Author,
				BodyText: article.Text, Language: article.Language, WordCount: article.WordCount,
				PublishedAt: article.PublishedAt, FetchedAt: fetchedAt, CreatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&version).Error; err != nil {
				return err
			}
		}
		updates := map[string]interface{}{
			"final_url": finalURL, "canonical_url": canonicalURL,
			"title": strings.TrimSpace(article.Title), "body_text": article.Text,
			"author": strings.TrimSpace(article.Author), "language": article.Language,
			"word_count": article.WordCount, "content_hash": contentHash,
			"content_version": contentVersion, "fetched_at": fetchedAt, "extracted_at": now,
			"processing_state": DiscoveryProcessingReady, "processing_attempts": 0,
			"processing_error": "", "processing_error_type": "", "next_processing_at": nil,
			"eligibility_state": DiscoveryEligibilityUnknown, "eligibility_reasons": "dedupe_pending,assessment_pending",
			"dedupe_state": DiscoveryDedupePending, "assessment_state": DiscoveryAssessmentPending,
			"updated_at": now,
		}
		if summary := archive.BuildSummary(article.Description, 260); summary != "" {
			updates["summary"] = summary
		}
		if article.PublishedAt != nil {
			updates["published_at"] = article.PublishedAt
			updates["published_confidence"] = "article_metadata"
		}
		if bodyChanged {
			updates["body_changed_at"] = now
		}
		return tx.Model(&current).Updates(updates).Error
	})
}

func resolveArticleCanonicalURL(finalURL string, canonicalURL string) string {
	base, err := url.Parse(strings.TrimSpace(finalURL))
	if err != nil {
		return strings.TrimSpace(finalURL)
	}
	canonicalURL = strings.TrimSpace(canonicalURL)
	if canonicalURL == "" {
		canonicalURL = base.String()
	} else if parsed, parseErr := url.Parse(canonicalURL); parseErr == nil {
		canonicalURL = base.ResolveReference(parsed).String()
	}
	if normalized, normalizeErr := NormalizeArticleURL(canonicalURL); normalizeErr == nil {
		return normalized
	}
	return canonicalURL
}
