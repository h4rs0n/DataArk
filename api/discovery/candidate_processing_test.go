package discovery

import (
	"DataArk/config"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProcessCandidateExtractsVersionsAndHandsOffAssessment(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Date(2026, 7, 13, 16, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetch := fetchDiscoveryRequest
	oldMinimum := config.DISCOVERYARTICLEMINCHARS
	discoveryClock = clock
	config.DISCOVERYARTICLEMINCHARS = 80
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetch
		config.DISCOVERYARTICLEMINCHARS = oldMinimum
	})

	bodyText := strings.Repeat("This independently verifiable article presents evidence, data, measurements, experiments, examples, alternatives, counterexamples, a durable method, the underlying mechanism, and a practical conclusion. ", 10)
	body := articleHTML("First durable version", bodyText)
	fetchDiscoveryRequest = func(_ context.Context, request FetchRequest) (FetchResult, error) {
		if request.Kind != FetchKindArticle {
			t.Fatalf("fetch kind = %s", request.Kind)
		}
		return FetchResult{StatusCode: 200, FinalURL: "https://example.com/posts/1?utm_source=feed", ContentType: "text/html", Body: body, FetchedAt: clock.Now()}, nil
	}

	source := DiscoverySource{Name: "Feed", URL: "https://example.com/feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	write, err := upsertDiscoveryCandidate(source, discoveredCandidate{URL: "https://example.com/posts/1", Title: "Feed title", DiscoveryMethod: DiscoveryMethodFeed})
	if err != nil {
		t.Fatal(err)
	}
	if err := ProcessCandidate(context.Background(), write.Candidate.ID, "0"); err != nil {
		t.Fatal(err)
	}

	var candidate DiscoveryCandidate
	if err := db.First(&candidate, write.Candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ProcessingState != DiscoveryProcessingReady || candidate.EligibilityState != DiscoveryEligibilityUnknown {
		t.Fatalf("states = %q/%q", candidate.ProcessingState, candidate.EligibilityState)
	}
	if candidate.ContentVersion != 1 || candidate.ContentHash == "" || !strings.Contains(candidate.BodyText, "independently verifiable") {
		t.Fatalf("content version = %#v", candidate)
	}
	if candidate.Title != "First durable version" || candidate.Author != "Ada Example" || candidate.Language != "en" {
		t.Fatalf("extracted metadata = %#v", candidate)
	}
	if candidate.CanonicalURL != "https://example.com/posts/1" || candidate.PublishedAt == nil || candidate.PublishedConfidence != "article_metadata" {
		t.Fatalf("canonical/published metadata = %#v", candidate)
	}
	if candidate.DedupeState != DiscoveryDedupeReady || candidate.AssessmentState != DiscoveryAssessmentPending || candidate.CurrentAssessmentID != nil {
		t.Fatalf("downstream states = %q/%q", candidate.DedupeState, candidate.AssessmentState)
	}
	if candidate.EligibilityReasons != "assessment_pending" {
		t.Fatalf("eligibility reasons = %q", candidate.EligibilityReasons)
	}
	assertContentVersionCount(t, candidate.ID, 1)

	clock.Advance(time.Hour)
	if err := ProcessCandidate(context.Background(), candidate.ID, "1"); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ContentVersion != 1 {
		t.Fatalf("unchanged content version = %d, want 1", candidate.ContentVersion)
	}
	assertContentVersionCount(t, candidate.ID, 1)

	body = articleHTML("Second durable version", bodyText+strings.Repeat(" Materially new field observations change the conclusion.", 5))
	clock.Advance(time.Hour)
	if err := ProcessCandidate(context.Background(), candidate.ID, "1"); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ContentVersion != 2 || candidate.Title != "Second durable version" {
		t.Fatalf("changed candidate = %#v", candidate)
	}
	assertContentVersionCount(t, candidate.ID, 2)
	if err := ProcessCandidate(context.Background(), candidate.ID, "1"); err != nil {
		t.Fatal(err)
	}
	assertContentVersionCount(t, candidate.ID, 2)
}

func TestUpsertCandidateHashesLongURLIdentity(t *testing.T) {
	setupSQLiteDB(t)
	source := DiscoverySource{
		Name: "Community", URL: "https://discourse.gohugo.io/latest.rss",
		Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, Enabled: true,
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	rawURL := "https://discourse.gohugo.io/t/hugo-module-dart-sass-use-works-when-used-directly-but-fails-when-routed-through-another-scss-file/57267"
	if len(rawURL) <= 128 {
		t.Fatalf("fixture URL length = %d, want more than 128", len(rawURL))
	}

	first, err := upsertDiscoveryCandidate(source, discoveredCandidate{URL: rawURL, Title: "Long URL", DiscoveryMethod: DiscoveryMethodFeed})
	if err != nil {
		t.Fatal(err)
	}
	second, err := upsertDiscoveryCandidate(source, discoveredCandidate{URL: rawURL, Title: "Long URL", DiscoveryMethod: DiscoveryMethodFeed})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || second.Created || first.Candidate.ID != second.Candidate.ID {
		t.Fatalf("long URL writes first=%#v second=%#v", first, second)
	}
	normalizedURL, err := NormalizeArticleURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	wantKey := initialCandidateDedupeKey(normalizedURL)
	if first.Candidate.URL != rawURL || first.Candidate.NormalizedURL != normalizedURL || first.Candidate.DedupeKey != wantKey {
		t.Fatalf("long URL candidate = %#v, want dedupe key %q", first.Candidate, wantKey)
	}
	if len(first.Candidate.DedupeKey) > 128 {
		t.Fatalf("dedupe key length = %d, want at most 128", len(first.Candidate.DedupeKey))
	}
	var count int64
	if err := db.Model(&DiscoveryCandidate{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("candidate count = %d, want 1", count)
	}
	if err := db.Model(&DiscoveryCandidate{}).Where("id = ?", first.Candidate.ID).Update("dedupe_key", "").Error; err != nil {
		t.Fatal(err)
	}
	third, err := upsertDiscoveryCandidate(source, discoveredCandidate{URL: rawURL, Title: "Long URL", DiscoveryMethod: DiscoveryMethodFeed})
	if err != nil {
		t.Fatal(err)
	}
	if third.Created || third.Candidate.DedupeKey != wantKey {
		t.Fatalf("empty dedupe key repair = %#v, want %q", third, wantKey)
	}
}

func TestParseFeedCandidatesSkipsListingURLs(t *testing.T) {
	body := []byte(`<?xml version="1.0"?><rss version="2.0"><channel>
<title>Notes</title>
<item><title>Go</title><link>https://example.com/tag/go</link></item>
<item><title>Sign in</title><link>https://example.com/login</link></item>
<item><title>Durable note</title><link>https://example.com/posts/durable-note</link></item>
</channel></rss>`)
	items, err := parseFeedCandidates(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].URL != "https://example.com/posts/durable-note" {
		t.Fatalf("feed listing URLs leaked into candidates: %#v", items)
	}
}

func TestProcessCandidateRejectsListingURLWithoutFetch(t *testing.T) {
	setupSQLiteDB(t)
	fetched := false
	oldFetch := fetchDiscoveryRequest
	fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) {
		fetched = true
		return FetchResult{}, errors.New("listing URLs must not be fetched")
	}
	t.Cleanup(func() { fetchDiscoveryRequest = oldFetch })
	candidate := createProcessingCandidate(t, "https://example.com/tag/go")
	if err := ProcessCandidate(context.Background(), candidate.ID, "0"); err != nil {
		t.Fatal(err)
	}
	if fetched {
		t.Fatal("listing URL was fetched before the admission gate")
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ProcessingState != DiscoveryProcessingIneligible || candidate.ProcessingErrorType != processingErrorNotArticle {
		t.Fatalf("listing URL state = %#v", candidate)
	}
	if candidate.AssessmentState == DiscoveryAssessmentReady || candidate.CurrentAssessmentID != nil {
		t.Fatalf("listing URL must not enter assessment: %#v", candidate)
	}
}

func TestProcessCandidateClassifiesNonArticlesAndShortBodies(t *testing.T) {
	setupSQLiteDB(t)
	oldFetch := fetchDiscoveryRequest
	oldMinimum := config.DISCOVERYARTICLEMINCHARS
	config.DISCOVERYARTICLEMINCHARS = 100
	t.Cleanup(func() {
		fetchDiscoveryRequest = oldFetch
		config.DISCOVERYARTICLEMINCHARS = oldMinimum
	})

	pages := map[string][]byte{
		"https://example.com/tag/go":     []byte(`<html lang="en"><head><title>Tag: Go</title></head><body><main><h1>Posts tagged Go</h1><a href="/posts/1">Post</a></main></body></html>`),
		"https://example.com/login":      []byte(`<html lang="en"><head><title>Sign in</title></head><body><form><input type="password"></form></body></html>`),
		"https://example.com/posts/tiny": []byte(`<html lang="en"><head><title>Tiny note</title></head><body><article><h1>Tiny note</h1><p>Too short.</p></article></body></html>`),
	}
	fetchDiscoveryRequest = func(_ context.Context, request FetchRequest) (FetchResult, error) {
		return FetchResult{StatusCode: 200, FinalURL: request.URL, ContentType: "text/html", Body: pages[request.URL], FetchedAt: time.Now()}, nil
	}
	for rawURL, wantCategory := range map[string]string{
		"https://example.com/tag/go":     processingErrorNotArticle,
		"https://example.com/login":      processingErrorNotArticle,
		"https://example.com/posts/tiny": processingErrorBodyTooShort,
	} {
		candidate := createProcessingCandidate(t, rawURL)
		if err := ProcessCandidate(context.Background(), candidate.ID, "0"); err != nil {
			t.Fatal(err)
		}
		if err := db.First(&candidate, candidate.ID).Error; err != nil {
			t.Fatal(err)
		}
		if candidate.ProcessingState != DiscoveryProcessingIneligible || candidate.EligibilityState != DiscoveryEligibilityIneligible || candidate.ProcessingErrorType != wantCategory {
			t.Fatalf("%s states = %q/%q category=%q", rawURL, candidate.ProcessingState, candidate.EligibilityState, candidate.ProcessingErrorType)
		}
	}
}

func TestProcessCandidateRetriesTransientFailuresAndStopsAtBound(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Date(2026, 7, 13, 18, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetch := fetchDiscoveryRequest
	oldMaximum := config.DISCOVERYPROCESSINGMAXATTEMPTS
	discoveryClock = clock
	config.DISCOVERYPROCESSINGMAXATTEMPTS = 2
	fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) {
		return FetchResult{}, context.DeadlineExceeded
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetch
		config.DISCOVERYPROCESSINGMAXATTEMPTS = oldMaximum
	})

	candidate := createProcessingCandidate(t, "https://example.com/posts/retry")
	if err := ProcessCandidate(context.Background(), candidate.ID, "0"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first error = %v", err)
	}
	if err := db.First(&candidate, candidate.ID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ProcessingState != DiscoveryProcessingFetchPending || candidate.NextProcessingAt == nil || !candidate.NextProcessingAt.After(clock.Now()) {
		t.Fatalf("retry state = %#v", candidate)
	}
	if err := ProcessCandidate(context.Background(), candidate.ID, "0"); err != nil {
		t.Fatalf("bounded final attempt = %v", err)
	}
	candidateID := candidate.ID
	candidate = DiscoveryCandidate{}
	if err := db.First(&candidate, candidateID).Error; err != nil {
		t.Fatal(err)
	}
	if candidate.ProcessingState != DiscoveryProcessingFailed || candidate.EligibilityState != DiscoveryEligibilityReview || candidate.ProcessingAttempts != 2 || candidate.NextProcessingAt != nil {
		t.Fatalf("final failure = %#v", candidate)
	}

	robots := createProcessingCandidate(t, "https://example.com/private/article")
	fetchDiscoveryRequest = func(context.Context, FetchRequest) (FetchResult, error) {
		return FetchResult{}, ErrRobotsDisallowed
	}
	if err := ProcessCandidate(context.Background(), robots.ID, "0"); err != nil {
		t.Fatal(err)
	}
	robotsID := robots.ID
	robots = DiscoveryCandidate{}
	if err := db.First(&robots, robotsID).Error; err != nil {
		t.Fatal(err)
	}
	if robots.ProcessingState != DiscoveryProcessingIneligible || robots.ProcessingErrorType != "robots_disallowed" || robots.NextProcessingAt != nil {
		t.Fatalf("robots state = %#v", robots)
	}
}

func TestFeedCandidatesReachTerminalProcessingStatesWithoutLLM(t *testing.T) {
	setupSQLiteDB(t)
	world := newDeterministicSiteWorld(t)
	oldFetch := fetchDiscoveryRequest
	oldMinimum := config.DISCOVERYARTICLEMINCHARS
	fetchDiscoveryRequest = world.Fetcher.Fetch
	config.DISCOVERYARTICLEMINCHARS = 120
	t.Cleanup(func() {
		fetchDiscoveryRequest = oldFetch
		config.DISCOVERYARTICLEMINCHARS = oldMinimum
	})

	response, err := world.Fetcher.Fetch(context.Background(), FetchRequest{URL: world.B.URL + "/feed.xml", Kind: FetchKindFeed})
	if err != nil {
		t.Fatal(err)
	}
	items, err := parseFeedCandidates(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	source := DiscoverySource{Name: "Thoughtful B", URL: world.B.URL + "/feed.xml", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	var ready, ineligible int
	for _, item := range items {
		item.DiscoveryMethod = DiscoveryMethodFeed
		write, writeErr := upsertDiscoveryCandidate(source, item)
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if processErr := ProcessCandidate(context.Background(), write.Candidate.ID, "0"); processErr != nil {
			t.Fatal(processErr)
		}
		var candidate DiscoveryCandidate
		if loadErr := db.First(&candidate, write.Candidate.ID).Error; loadErr != nil {
			t.Fatal(loadErr)
		}
		switch candidate.ProcessingState {
		case DiscoveryProcessingReady:
			ready++
		case DiscoveryProcessingIneligible:
			ineligible++
		default:
			t.Fatalf("feed candidate %s remained in state %q", candidate.URL, candidate.ProcessingState)
		}
	}
	if ready == 0 || ready+ineligible != len(items) {
		t.Fatalf("terminal feed candidates ready=%d ineligible=%d total=%d", ready, ineligible, len(items))
	}
}

func articleHTML(title string, body string) []byte {
	return []byte(`<html lang="en"><head><title>` + title + `</title><meta name="author" content="Ada Example"><meta property="article:published_time" content="2024-03-01T10:30:00Z"><link rel="canonical" href="/posts/1"></head><body><article><h1>` + title + `</h1><p>` + body + `</p></article></body></html>`)
}

func createProcessingCandidate(t *testing.T, rawURL string) DiscoveryCandidate {
	t.Helper()
	source := DiscoverySource{URL: "https://example.com/feed.xml"}
	if err := db.Where("url = ?", source.URL).Attrs(DiscoverySource{Name: "Fixture", Type: DiscoverySourceTypeFeed, Enabled: true}).FirstOrCreate(&source).Error; err != nil {
		t.Fatal(err)
	}
	candidate := DiscoveryCandidate{SourceID: source.ID, SourceName: source.Name, URL: rawURL, Status: DiscoveryCandidateStatusNew, ProcessingState: DiscoveryProcessingFetchPending, EligibilityState: DiscoveryEligibilityUnknown, LastSeenAt: time.Now()}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	return candidate
}

func assertContentVersionCount(t *testing.T, candidateID uint, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&DiscoveryArticleContentVersion{}).Where("candidate_id = ?", candidateID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("content version count = %d, want %d", count, want)
	}
}
