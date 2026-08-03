package discovery

import (
	"DataArk/config"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFeedConditionalFetchPersistsValidatorsAndSkipsUnchangedWork(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	discoveryClock = clock
	var calls int
	fetchDiscoveryRequest = func(_ context.Context, request FetchRequest) (FetchResult, error) {
		calls++
		if calls == 1 {
			if request.ETag != "" {
				t.Fatalf("initial ETag = %q", request.ETag)
			}
			return FetchResult{StatusCode: http.StatusOK, ETag: `"feed-v1"`, Body: []byte(`<?xml version="1.0"?><rss version="2.0"><channel><item><title>Only Post</title><link>https://example.com/only</link></item></channel></rss>`)}, nil
		}
		if request.ETag != `"feed-v1"` {
			t.Fatalf("conditional ETag = %q", request.ETag)
		}
		return FetchResult{StatusCode: http.StatusNotModified, ETag: `"feed-v1"`, NotModified: true}, nil
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetcher
	})

	source, err := CreateDiscoverySource("Conditional", "https://example.com/feed.xml", DiscoverySourceTypeFeed, true)
	if err != nil {
		t.Fatal(err)
	}
	first, err := FetchDiscoverySource(context.Background(), source)
	if err != nil || first.Stored != 1 {
		t.Fatalf("first fetch = %#v, %v", first, err)
	}
	clock.Advance(time.Hour)
	second, err := FetchDiscoverySource(context.Background(), source)
	if err != nil || second.Stored != 0 || second.Discovered != 0 {
		t.Fatalf("second fetch = %#v, %v", second, err)
	}
	var candidates int64
	if err := db.Model(&DiscoveryCandidate{}).Count(&candidates).Error; err != nil || candidates != 1 {
		t.Fatalf("candidate count = %d, %v", candidates, err)
	}
	var runs []DiscoveryFetchRun
	if err := db.Order("id").Find(&runs).Error; err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].NotModified || !runs[1].NotModified || runs[1].NewCount != 0 {
		t.Fatalf("fetch runs = %#v", runs)
	}
}

func TestRobotsRulesAreAdvisoryAndExposeSitemapHints(t *testing.T) {
	var robotsRequests atomic.Int32
	var articleRequests atomic.Int32
	var articleUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/robots.txt":
			robotsRequests.Add(1)
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte("User-agent: *\nDisallow: /private\nSitemap: " + serverURLForRequest(request) + "/sitemap.xml\n"))
		case "/private/article":
			articleRequests.Add(1)
			articleUserAgent = request.Header.Get("User-Agent")
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("article"))
		}
	}))
	t.Cleanup(server.Close)

	clock := &advancingClock{now: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}
	raw := &HTTPClientFetcher{Client: server.Client(), Clock: clock, Validator: allowTestURL}
	robots := NewRobotsCache(clock, time.Hour, raw)
	fetcher := &HTTPClientFetcher{Client: server.Client(), Clock: clock, Validator: allowTestURL, Robots: robots}
	result, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/private/article", Kind: FetchKindArticle})
	if err != nil {
		t.Fatal(err)
	}
	if robotsRequests.Load() != 1 || articleRequests.Load() != 1 {
		t.Fatalf("requests robots=%d article=%d", robotsRequests.Load(), articleRequests.Load())
	}
	if result.RobotsStatus != "available" || articleUserAgent != config.DefaultDiscoveryUserAgent {
		t.Fatalf("result status=%q user-agent=%q", result.RobotsStatus, articleUserAgent)
	}
	inspection := robots.Inspect(context.Background(), server.URL+"/another")
	if len(inspection.Sitemaps) != 1 || inspection.Sitemaps[0] != server.URL+"/sitemap.xml" || robotsRequests.Load() != 1 {
		t.Fatalf("cached robots inspection = %#v, requests=%d", inspection, robotsRequests.Load())
	}
}

func serverURLForRequest(request *http.Request) string {
	return "http://" + request.Host
}

func TestRobotsUnavailableDoesNotBlockManualDiscoveryRequest(t *testing.T) {
	var articleRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/robots.txt" {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		articleRequests.Add(1)
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte("article"))
	}))
	t.Cleanup(server.Close)

	clock := &advancingClock{now: time.Date(2026, 8, 3, 13, 0, 0, 0, time.UTC)}
	raw := &HTTPClientFetcher{Client: server.Client(), Clock: clock, Validator: allowTestURL}
	fetcher := &HTTPClientFetcher{
		Client: server.Client(), Clock: clock, Validator: allowTestURL,
		Robots: NewRobotsCache(clock, time.Hour, raw),
	}
	result, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/article", Kind: FetchKindArticle})
	if err != nil {
		t.Fatal(err)
	}
	if result.RobotsStatus != "unavailable" || articleRequests.Load() != 1 {
		t.Fatalf("result status=%q article requests=%d", result.RobotsStatus, articleRequests.Load())
	}
}

func TestRedirectRevalidatesTargetAndContentTypeIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/redirect":
			http.Redirect(writer, request, "/private", http.StatusFound)
		case "/private":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("must not be reached"))
		case "/wrong":
			writer.Header().Set("Content-Type", "image/png")
			_, _ = writer.Write([]byte("not html"))
		}
	}))
	t.Cleanup(server.Close)
	validator := func(_ context.Context, rawURL string) (*neturl.URL, error) {
		parsed, err := neturl.Parse(rawURL)
		if err == nil && parsed.Path == "/private" {
			return nil, ErrUnsafeIPAddress
		}
		return parsed, err
	}
	fetcher := HTTPClientFetcher{Client: server.Client(), Validator: validator}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/redirect", Kind: FetchKindHTML}); !errors.Is(err, ErrUnsafeIPAddress) {
		t.Fatalf("redirect error = %v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/wrong", Kind: FetchKindHTML}); !errors.Is(err, ErrHTTPFetchContentType) {
		t.Fatalf("content-type error = %v", err)
	}
}

func TestFailureBackoffRecoversAndLowHitSchedulesRemainFinite(t *testing.T) {
	setupSQLiteDB(t)
	clock := &advancingClock{now: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}
	oldClock := discoveryClock
	oldFetcher := fetchDiscoveryRequest
	discoveryClock = clock
	var fail = true
	fetchDiscoveryRequest = func(_ context.Context, _ FetchRequest) (FetchResult, error) {
		if fail {
			return FetchResult{}, context.DeadlineExceeded
		}
		return FetchResult{StatusCode: http.StatusOK, Body: []byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`)}, nil
	}
	t.Cleanup(func() {
		discoveryClock = oldClock
		fetchDiscoveryRequest = oldFetcher
	})
	source, err := CreateDiscoverySource("Recovery", "https://example.com/feed.xml", DiscoverySourceTypeFeed, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FetchDiscoverySource(context.Background(), source); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failure = %v", err)
	}
	if source.FailureCount != 1 || source.BackoffUntil == nil || !source.BackoffUntil.After(clock.Now()) {
		t.Fatalf("failed source = %#v", source)
	}
	clock.Advance(2 * time.Hour)
	fail = false
	if _, err := FetchDiscoverySource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if source.FailureCount != 0 || source.BackoffUntil != nil || source.NextDueAt == nil || !source.NextDueAt.After(clock.Now()) {
		t.Fatalf("recovered source = %#v", source)
	}

	policy := SourceSchedulePolicy{Clock: clock, ObservingInterval: 7 * 24 * time.Hour, DormantInterval: 30 * 24 * time.Hour}
	next := policy.NextSuccess(DiscoverySource{EndpointType: "homepage"}, "dormant", false)
	if next.IsZero() || !next.Equal(clock.Now().Add(30*24*time.Hour)) {
		t.Fatalf("low-hit next due = %s", next)
	}
}

func TestHostLimiterEnforcesConcurrencyAndMinimumInterval(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}
	var mu sync.Mutex
	var delays []time.Duration
	limiter := NewHostLimiter(1, 3*time.Second, clock, func(_ context.Context, delay time.Duration) error {
		mu.Lock()
		delays = append(delays, delay)
		mu.Unlock()
		clock.Advance(delay)
		return nil
	})
	releaseFirst, err := limiter.Acquire(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan func(), 1)
	go func() {
		release, acquireErr := limiter.Acquire(context.Background(), "example.com")
		if acquireErr == nil {
			acquired <- release
		}
	}()
	select {
	case <-acquired:
		t.Fatal("second request bypassed concurrency limit")
	case <-time.After(20 * time.Millisecond):
	}
	releaseFirst()
	select {
	case releaseSecond := <-acquired:
		releaseSecond()
	case <-time.After(time.Second):
		t.Fatal("second request did not acquire after release")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(delays) != 1 || delays[0] != 3*time.Second {
		t.Fatalf("delays = %v", delays)
	}
}
