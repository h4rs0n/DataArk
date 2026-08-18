package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"sync"
	"testing"
	"time"
)

type advancingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *advancingClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *advancingClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

type recordedJobs struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

type recordingJobEnqueuer struct {
	store *recordedJobs
}

func allowTestURL(_ context.Context, rawURL string) (*neturl.URL, error) {
	return neturl.Parse(rawURL)
}

func (queue recordingJobEnqueuer) enqueue(key string) error {
	queue.store.mu.Lock()
	defer queue.store.mu.Unlock()
	queue.store.keys[key] = struct{}{}
	return nil
}

func (queue recordingJobEnqueuer) EnqueueFetchSource(_ context.Context, sourceID uint) error {
	return queue.enqueue(fmt.Sprintf("fetch:%d", sourceID))
}

func (queue recordingJobEnqueuer) EnqueueScanBlogroll(_ context.Context, siteID uint) error {
	return queue.enqueue(fmt.Sprintf("blogroll:%d", siteID))
}

func (queue recordingJobEnqueuer) EnqueueBackfillSite(_ context.Context, siteID uint) error {
	return queue.enqueue(fmt.Sprintf("backfill:%d", siteID))
}

func (queue recordingJobEnqueuer) EnqueueProcessCandidate(_ context.Context, candidateID uint, contentVersion string) error {
	return queue.enqueue(fmt.Sprintf("candidate:%d:%s", candidateID, contentVersion))
}

func (queue recordingJobEnqueuer) EnqueueAssessArticle(_ context.Context, candidateID uint, contentVersion string) error {
	return queue.enqueue(fmt.Sprintf("assess:%d:%s", candidateID, contentVersion))
}

func (queue recordingJobEnqueuer) EnqueueGenerateDaily(_ context.Context, userID uint, localDate string) error {
	return queue.enqueue(fmt.Sprintf("daily:%d:%s", userID, localDate))
}

func TestDeterministicBoundariesAdvanceTimeAndSurviveQueueRestart(t *testing.T) {
	clock := &advancingClock{now: time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)}
	clock.Advance(90 * time.Minute)
	if got := clock.Now(); !got.Equal(time.Date(2026, 7, 13, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("clock now = %s", got)
	}

	store := &recordedJobs{keys: make(map[string]struct{})}
	firstProcess := recordingJobEnqueuer{store: store}
	if err := firstProcess.EnqueueFetchSource(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	secondProcess := recordingJobEnqueuer{store: store}
	if err := secondProcess.EnqueueFetchSource(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(store.keys) != 1 {
		t.Fatalf("unique jobs after restart = %d, want 1", len(store.keys))
	}
}

func TestHTTPClientFetcherSupportsValidatorsRedirectsAndBodyLimits(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch request.URL.Path {
		case "/redirect":
			http.Redirect(writer, request, "/etag", http.StatusFound)
		case "/etag":
			writer.Header().Set("ETag", `"fixture-v1"`)
			writer.Header().Set("Last-Modified", "Mon, 13 Jul 2026 00:00:00 GMT")
			if request.Header.Get("If-None-Match") == `"fixture-v1"` {
				writer.WriteHeader(http.StatusNotModified)
				return
			}
			_, _ = writer.Write([]byte("fixture body"))
		case "/large":
			_, _ = writer.Write([]byte("12345"))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	clock := &advancingClock{now: time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)}
	fetcher := HTTPClientFetcher{Client: server.Client(), Clock: clock, Validator: allowTestURL}
	first, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/redirect", Kind: FetchKindHTML, MaxBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	if first.StatusCode != http.StatusOK || first.FinalURL != server.URL+"/etag" || string(first.Body) != "fixture body" {
		t.Fatalf("first response = %#v", first)
	}
	if first.ETag != `"fixture-v1"` || first.LastModified == "" || !first.FetchedAt.Equal(clock.Now()) {
		t.Fatalf("validator result = %#v", first)
	}

	second, err := fetcher.Fetch(context.Background(), FetchRequest{
		URL:      server.URL + "/etag",
		ETag:     first.ETag,
		Kind:     FetchKindHTML,
		MaxBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.StatusCode != http.StatusNotModified || !second.NotModified || len(second.Body) != 0 {
		t.Fatalf("conditional response = %#v", second)
	}
	if requests != 3 {
		t.Fatalf("request count = %d, want redirect + two fetches", requests)
	}

	_, err = fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/large", Kind: FetchKindHTML, MaxBytes: 4})
	if !errors.Is(err, ErrHTTPFetchBodyTooLarge) {
		t.Fatalf("large response error = %v", err)
	}
}
