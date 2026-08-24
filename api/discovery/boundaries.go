package discovery

import (
	"DataArk/config"
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
	defaultFeedFetchBodyLimit    int64 = 8 << 20
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

// Timestamp 把发现模块的可注入时钟暴露给评估包，保证测试冻结时间一致。
func Timestamp() time.Time { return discoveryClock.Now() }

type FetchKind string

const (
	FetchKindHTML    FetchKind = "html"
	FetchKindFeed    FetchKind = "feed"
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

type RobotsInspector interface {
	Inspect(context.Context, string) RobotsInspection
}

// HTTPClientFetcher 是生产抓取器。robots.txt 只用于诊断状态，其 Allow/Disallow
// 规则不会拦截 owner 手动触发的发现请求，也不再读取 Sitemap 提示。
type HTTPClientFetcher struct {
	Client       *http.Client
	Clock        Clock
	Validator    URLValidator
	Limiter      *HostLimiter
	Robots       RobotsInspector
	UserAgent    string
	MaxRedirects int
	BlockURL     func(context.Context, string) error
}

func (fetcher HTTPClientFetcher) Fetch(ctx context.Context, input FetchRequest) (FetchResult, error) {
	if fetcher.BlockURL != nil {
		if err := fetcher.BlockURL(ctx, input.URL); err != nil {
			return FetchResult{}, withFetchDiagnostic(err, input.URL, 0, discoveryFetchErrorCategory(err))
		}
	}
	validator := fetcher.Validator
	if validator == nil {
		validator = ValidateFetchURL
	}
	validatedURL, err := validator(ctx, input.URL)
	if err != nil {
		return FetchResult{}, withFetchDiagnostic(err, input.URL, 0, discoveryFetchErrorCategory(err))
	}
	userAgent := strings.TrimSpace(fetcher.UserAgent)
	if userAgent == "" {
		userAgent = config.DefaultDiscoveryUserAgent
	}
	robotsStatus := ""
	if fetcher.Robots != nil && input.Kind != FetchKindRobots {
		robotsStatus = fetcher.Robots.Inspect(ctx, validatedURL.String()).Status
	}
	if fetcher.Limiter != nil {
		release, err := fetcher.Limiter.Acquire(ctx, validatedURL.Hostname())
		if err != nil {
			return FetchResult{}, withFetchDiagnostic(err, validatedURL.String(), 0, discoveryFetchErrorCategory(err))
		}
		defer release()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, validatedURL.String(), nil)
	if err != nil {
		return FetchResult{}, withFetchDiagnostic(err, validatedURL.String(), 0, discoveryFetchErrorCategory(err))
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
		if fetcher.BlockURL != nil {
			if err := fetcher.BlockURL(request.Context(), request.URL.String()); err != nil {
				return err
			}
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
		failedURL := validatedURL.String()
		var requestError *neturl.Error
		if errors.As(err, &requestError) && strings.TrimSpace(requestError.URL) != "" {
			failedURL = requestError.URL
		}
		return FetchResult{}, withFetchDiagnostic(err, failedURL, 0, discoveryFetchErrorCategory(err))
	}
	defer response.Body.Close()
	responseURL := validatedURL.String()
	if response.Request != nil && response.Request.URL != nil {
		responseURL = response.Request.URL.String()
	}

	limit := input.MaxBytes
	if limit <= 0 {
		limit = defaultFetchBodyLimit(input.Kind)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		errorType := discoveryFetchErrorCategory(err)
		if errorType == "processing" {
			errorType = "network"
		}
		return FetchResult{}, withFetchDiagnostic(err, responseURL, response.StatusCode, errorType)
	}
	if int64(len(body)) > limit {
		return FetchResult{}, withFetchDiagnostic(ErrHTTPFetchBodyTooLarge, responseURL, response.StatusCode, "body_too_large")
	}
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices && response.StatusCode != http.StatusNotModified {
		if !allowedContentType(input.Kind, contentType, body) {
			contentTypeError := fmt.Errorf("%w: %s for %s", ErrHTTPFetchContentType, contentType, input.Kind)
			return FetchResult{}, withFetchDiagnostic(contentTypeError, responseURL, response.StatusCode, "content_type")
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
		return "application/rss+xml, application/atom+xml, application/rdf+xml, application/feed+json, application/json, application/xml, text/xml;q=0.9"
	case FetchKindRobots:
		return "text/plain, */*;q=0.1"
	default:
		return "text/html, application/xhtml+xml;q=0.9"
	}
}

func allowedContentType(kind FetchKind, value string, body []byte) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return kind == FetchKindFeed && strings.TrimSpace(value) == "" && looksLikeFeed(body)
	}
	mediaType = strings.ToLower(mediaType)
	switch kind {
	case FetchKindFeed:
		switch mediaType {
		case "application/rss+xml", "application/atom+xml", "application/rdf+xml", "application/feed+json", "application/json", "application/xml", "text/xml":
			return true
		case "text/plain", "text/html", "application/octet-stream":
			return looksLikeFeed(body)
		default:
			return false
		}
	case FetchKindRobots:
		return mediaType == "text/plain" || mediaType == "text/robots"
	default:
		return mediaType == "text/html" || mediaType == "application/xhtml+xml"
	}
}

func looksLikeFeed(body []byte) bool {
	prefix := body
	if len(prefix) > 4096 {
		prefix = prefix[:4096]
	}
	value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(string(prefix), "\ufeff")))
	if strings.HasPrefix(value, "{") {
		return strings.Contains(value, `"version"`) && strings.Contains(value, "jsonfeed.org/version/")
	}
	return strings.Contains(value, "<rss") || strings.Contains(value, "<feed") || strings.Contains(value, "<rdf:rdf")
}

// DiscoveryJobEnqueuer 只投递发现流水线作业：抓取、blogroll、回填、候选处理。
// 评估与日报走 jobqueue.JobEnqueuer，避免与全量队列同名，也不把日报方法暴露给发现包。
type DiscoveryJobEnqueuer interface {
	EnqueueFetchSource(context.Context, uint) error
	EnqueueScanBlogroll(context.Context, uint) error
	EnqueueBackfillSite(context.Context, uint) error
	EnqueueProcessCandidate(context.Context, uint, string) error
}
