package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

const (
	defaultHTMLFetchBodyLimit    int64 = 4 << 20
	defaultFeedFetchBodyLimit    int64 = 2 << 20
	defaultSitemapFetchBodyLimit int64 = 8 << 20
	defaultArticleFetchBodyLimit int64 = 8 << 20
	defaultRobotsFetchBodyLimit  int64 = 512 << 10
)

var ErrHTTPFetchBodyTooLarge = errors.New("http fetch response exceeds body limit")
var ErrHTTPFetchContentType = errors.New("http fetch response has an unsupported content type")
var ErrHTTPFetchStatus = errors.New("http fetch response has an unsuccessful status")
var ErrTooManyRedirects = errors.New("too many discovery redirects")
var ErrRobotsDisallowed = errors.New("robots.txt disallows discovery request")
var ErrRobotsUnavailable = errors.New("robots.txt could not be fetched safely")

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
	FetchKindRobots  FetchKind = "robots"
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
	RobotsStatus string
	Body         []byte
	FetchedAt    time.Time
}

// HTTPFetcher separates network I/O from discovery parsing and scheduling.
type HTTPFetcher interface {
	Fetch(context.Context, FetchRequest) (FetchResult, error)
}

type URLValidator func(context.Context, string) (*neturl.URL, error)

type RobotsChecker interface {
	Allowed(context.Context, string, string) (bool, string, error)
}

// HTTPClientFetcher is the minimal production HTTPFetcher. Safe URL validation,
// robots enforcement, redirects and per-host limiting are added by milestone M3.
type HTTPClientFetcher struct {
	Client       *http.Client
	Clock        Clock
	Validator    URLValidator
	Limiter      *HostLimiter
	Robots       RobotsChecker
	UserAgent    string
	MaxRedirects int
}

func (fetcher HTTPClientFetcher) Fetch(ctx context.Context, input FetchRequest) (FetchResult, error) {
	validator := fetcher.Validator
	if validator == nil {
		validator = ValidateFetchURL
	}
	validatedURL, err := validator(ctx, input.URL)
	if err != nil {
		return FetchResult{}, err
	}
	userAgent := strings.TrimSpace(fetcher.UserAgent)
	if userAgent == "" {
		userAgent = "DataArkDiscovery/1.0"
	}
	robotsStatus := ""
	if fetcher.Robots != nil && input.Kind != FetchKindRobots {
		allowed, status, err := fetcher.Robots.Allowed(ctx, validatedURL.String(), userAgent)
		if err != nil {
			return FetchResult{}, err
		}
		robotsStatus = status
		if !allowed {
			return FetchResult{}, ErrRobotsDisallowed
		}
	}
	if fetcher.Limiter != nil {
		release, err := fetcher.Limiter.Acquire(ctx, validatedURL.Hostname())
		if err != nil {
			return FetchResult{}, err
		}
		defer release()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, validatedURL.String(), nil)
	if err != nil {
		return FetchResult{}, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", acceptHeader(input.Kind))
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
	clientCopy := *client
	maxRedirects := fetcher.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = 5
	}
	previousRedirectCheck := client.CheckRedirect
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return ErrTooManyRedirects
		}
		if _, err := validator(request.Context(), request.URL.String()); err != nil {
			return err
		}
		if previousRedirectCheck != nil {
			return previousRedirectCheck(request, via)
		}
		return nil
	}
	response, err := clientCopy.Do(request)
	if err != nil {
		return FetchResult{}, err
	}
	defer response.Body.Close()

	limit := input.MaxBytes
	if limit <= 0 {
		limit = defaultFetchBodyLimit(input.Kind)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return FetchResult{}, err
	}
	if int64(len(body)) > limit {
		return FetchResult{}, ErrHTTPFetchBodyTooLarge
	}
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices && response.StatusCode != http.StatusNotModified {
		if !allowedContentType(input.Kind, contentType) {
			return FetchResult{}, fmt.Errorf("%w: %s for %s", ErrHTTPFetchContentType, contentType, input.Kind)
		}
	}

	clock := fetcher.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	return FetchResult{
		StatusCode:   response.StatusCode,
		FinalURL:     response.Request.URL.String(),
		ContentType:  contentType,
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
		NotModified:  response.StatusCode == http.StatusNotModified,
		RobotsStatus: robotsStatus,
		Body:         body,
		FetchedAt:    clock.Now(),
	}, nil
}

func defaultFetchBodyLimit(kind FetchKind) int64 {
	switch kind {
	case FetchKindFeed:
		return defaultFeedFetchBodyLimit
	case FetchKindSitemap:
		return defaultSitemapFetchBodyLimit
	case FetchKindArticle:
		return defaultArticleFetchBodyLimit
	case FetchKindRobots:
		return defaultRobotsFetchBodyLimit
	default:
		return defaultHTMLFetchBodyLimit
	}
}

func acceptHeader(kind FetchKind) string {
	switch kind {
	case FetchKindFeed:
		return "application/rss+xml, application/atom+xml, application/feed+json, application/json, application/xml, text/xml;q=0.9"
	case FetchKindSitemap:
		return "application/xml, text/xml, text/plain;q=0.8"
	case FetchKindRobots:
		return "text/plain, */*;q=0.1"
	default:
		return "text/html, application/xhtml+xml;q=0.9"
	}
}

func allowedContentType(kind FetchKind, value string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	switch kind {
	case FetchKindFeed:
		return mediaType == "application/rss+xml" || mediaType == "application/atom+xml" || mediaType == "application/feed+json" || mediaType == "application/json" || mediaType == "application/xml" || mediaType == "text/xml"
	case FetchKindSitemap:
		return mediaType == "application/xml" || mediaType == "text/xml" || mediaType == "text/plain"
	case FetchKindRobots:
		return mediaType == "text/plain" || mediaType == "text/robots"
	default:
		return mediaType == "text/html" || mediaType == "application/xhtml+xml"
	}
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
