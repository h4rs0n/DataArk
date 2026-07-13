package discovery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

const defaultHTTPFetchBodyLimit int64 = 4 << 20

var ErrHTTPFetchBodyTooLarge = errors.New("http fetch response exceeds body limit")

// Clock isolates wall-clock time from discovery services that need deterministic tests.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type FetchKind string

const (
	FetchKindHTML    FetchKind = "html"
	FetchKindFeed    FetchKind = "feed"
	FetchKindSitemap FetchKind = "sitemap"
	FetchKindArticle FetchKind = "article"
)

// FetchRequest describes the bounded portion of an HTTP request that discovery jobs may control.
type FetchRequest struct {
	URL          string
	ETag         string
	LastModified string
	Kind         FetchKind
	MaxBytes     int64
}

// FetchResult contains the data needed by parsers and conditional-fetch scheduling.
type FetchResult struct {
	StatusCode   int
	FinalURL     string
	ContentType  string
	ETag         string
	LastModified string
	NotModified  bool
	Body         []byte
	FetchedAt    time.Time
}

// HTTPFetcher separates network I/O from discovery parsing and scheduling.
type HTTPFetcher interface {
	Fetch(context.Context, FetchRequest) (FetchResult, error)
}

// HTTPClientFetcher is the minimal production HTTPFetcher. Safe URL validation,
// robots enforcement, redirects and per-host limiting are added by milestone M3.
type HTTPClientFetcher struct {
	Client *http.Client
	Clock  Clock
}

func (fetcher HTTPClientFetcher) Fetch(ctx context.Context, input FetchRequest) (FetchResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, input.URL, nil)
	if err != nil {
		return FetchResult{}, err
	}
	if input.ETag != "" {
		request.Header.Set("If-None-Match", input.ETag)
	}
	if input.LastModified != "" {
		request.Header.Set("If-Modified-Since", input.LastModified)
	}

	client := fetcher.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return FetchResult{}, err
	}
	defer response.Body.Close()

	limit := input.MaxBytes
	if limit <= 0 {
		limit = defaultHTTPFetchBodyLimit
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return FetchResult{}, err
	}
	if int64(len(body)) > limit {
		return FetchResult{}, ErrHTTPFetchBodyTooLarge
	}

	clock := fetcher.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	return FetchResult{
		StatusCode:   response.StatusCode,
		FinalURL:     response.Request.URL.String(),
		ContentType:  response.Header.Get("Content-Type"),
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
		NotModified:  response.StatusCode == http.StatusNotModified,
		Body:         body,
		FetchedAt:    clock.Now(),
	}, nil
}

// JobEnqueuer accepts only stable identifiers. Implementations must make each
// operation idempotent; large page or article bodies remain in storage.
type JobEnqueuer interface {
	EnqueueFetchSource(context.Context, uint) error
	EnqueueScanBlogroll(context.Context, uint) error
	EnqueueBackfillSite(context.Context, uint) error
	EnqueueProcessCandidate(context.Context, uint, string) error
	EnqueueGenerateDaily(context.Context, uint, string) error
}
