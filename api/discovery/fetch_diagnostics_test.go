package discovery

import (
	"DataArk/observability"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type diagnosticRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip diagnosticRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestHTTPStatusDiagnosticUsesFinalRedirectHostname(t *testing.T) {
	finalServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer finalServer.Close()
	finalURL := strings.Replace(finalServer.URL, "127.0.0.1", "localhost", 1)
	redirectServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, finalURL+"/private?token=secret", http.StatusFound)
	}))
	defer redirectServer.Close()

	fetcher := HTTPClientFetcher{Client: finalServer.Client(), Validator: allowTestURL}
	result, err := fetcher.Fetch(context.Background(), FetchRequest{URL: redirectServer.URL, Kind: FetchKindHTML})
	if err != nil {
		t.Fatal(err)
	}
	statusError := withHTTPStatusDiagnostic(fmt.Errorf("%w: %d", ErrHTTPFetchStatus, result.StatusCode), result, redirectServer.URL)
	if !errors.Is(statusError, ErrHTTPFetchStatus) {
		t.Fatalf("errors.Is failed for %v", statusError)
	}
	var detailer observability.FailureDetailer
	if !errors.As(statusError, &detailer) {
		t.Fatalf("missing failure details in %T", statusError)
	}
	details := detailer.ObservabilityFailure()
	if details.ErrorType != "http_status" || details.Domain != "localhost" || details.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("details = %#v", details)
	}
}

func TestHTTPClientFetcherAddsDomainToTimeout(t *testing.T) {
	fetcher := HTTPClientFetcher{
		Client: &http.Client{Transport: diagnosticRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		})},
		Validator: allowTestURL,
	}
	_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: "https://feed.example.com/private?token=secret", Kind: FetchKindFeed})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	var detailer observability.FailureDetailer
	if !errors.As(err, &detailer) {
		t.Fatalf("missing failure details in %T", err)
	}
	details := detailer.ObservabilityFailure()
	if details.ErrorType != "timeout" || details.Domain != "feed.example.com" || details.HTTPStatus != 0 {
		t.Fatalf("details = %#v", details)
	}
	event := observability.WithError(observability.Event{Status: "failed"}, err)
	if strings.Contains(event.ErrorMessage, "feed.example.com") || strings.Contains(event.ErrorMessage, "token=secret") {
		t.Fatalf("unsafe timeout message = %q", event.ErrorMessage)
	}
}

func TestResponseValidationDiagnosticsIncludeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/large":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte("larger than the configured limit"))
		case "/binary":
			writer.Header().Set("Content-Type", "application/octet-stream")
			_, _ = writer.Write([]byte("binary"))
		}
	}))
	defer server.Close()
	fetcher := HTTPClientFetcher{Client: server.Client(), Validator: allowTestURL}
	for _, testCase := range []struct {
		name      string
		path      string
		maxBytes  int64
		errorType string
		cause     error
	}{
		{name: "body too large", path: "/large", maxBytes: 1, errorType: "body_too_large", cause: ErrHTTPFetchBodyTooLarge},
		{name: "content type", path: "/binary", errorType: "content_type", cause: ErrHTTPFetchContentType},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + testCase.path, Kind: FetchKindHTML, MaxBytes: testCase.maxBytes})
			if !errors.Is(err, testCase.cause) {
				t.Fatalf("error = %v, want %v", err, testCase.cause)
			}
			var detailer observability.FailureDetailer
			if !errors.As(err, &detailer) {
				t.Fatalf("missing failure details in %T", err)
			}
			details := detailer.ObservabilityFailure()
			if details.ErrorType != testCase.errorType || details.Domain != "127.0.0.1" || details.HTTPStatus != http.StatusOK {
				t.Fatalf("details = %#v", details)
			}
		})
	}
}

func TestFeedResponseCompatibilityRemainsBounded(t *testing.T) {
	rdfBody := []byte(`<?xml version="1.0"?><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/"><channel rdf:about="https://example.com/feed"><title>RDF</title><link>https://example.com/</link><description>RDF</description></channel><item rdf:about="https://example.com/post"><title>Post</title><link>https://example.com/post</link></item></rdf:RDF>`)
	rssBody := []byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>RSS</title></channel></rss>`)
	largeRSSBody := []byte(`<rss version="2.0"><channel><description>` + strings.Repeat("x", (2<<20)+(32<<10)) + `</description></channel></rss>`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rdf":
			writer.Header().Set("Content-Type", "application/rdf+xml; charset=UTF-8")
			_, _ = writer.Write(rdfBody)
		case "/generic":
			writer.Header().Set("Content-Type", "application/octet-stream")
			_, _ = writer.Write(rssBody)
		case "/large":
			writer.Header().Set("Content-Type", "application/rss+xml")
			_, _ = writer.Write(largeRSSBody)
		case "/html":
			writer.Header().Set("Content-Type", "text/html")
			_, _ = writer.Write([]byte(`<html><body>not a feed</body></html>`))
		}
	}))
	defer server.Close()

	fetcher := HTTPClientFetcher{Client: server.Client(), Validator: allowTestURL}
	for _, path := range []string{"/rdf", "/generic", "/large"} {
		if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + path, Kind: FetchKindFeed}); err != nil {
			t.Fatalf("fetch %s: %v", path, err)
		}
	}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL + "/html", Kind: FetchKindFeed}); !errors.Is(err, ErrHTTPFetchContentType) {
		t.Fatalf("HTML feed response error = %v", err)
	}
	if defaultFetchBodyLimit(FetchKindFeed) != 8<<20 {
		t.Fatalf("feed body limit = %d, want %d", defaultFetchBodyLimit(FetchKindFeed), 8<<20)
	}
}

func TestFeedParseFailureIsSafeAndCategorized(t *testing.T) {
	setupSQLiteDB(t)
	oldFetcher := fetchDiscoveryRequest
	fetchDiscoveryRequest = func(_ context.Context, _ FetchRequest) (FetchResult, error) {
		return FetchResult{
			StatusCode: http.StatusOK, FinalURL: "https://example.com/wp-json/wp/v2/pages/7", ContentType: "application/json",
			Body: []byte(`{"title":{"rendered":"private response excerpt"},"link":"https://example.com/"}`),
		}, nil
	}
	t.Cleanup(func() { fetchDiscoveryRequest = oldFetcher })

	source := DiscoverySource{Name: "Invalid metadata", URL: "https://example.com/wp-json/wp/v2/pages/7", Type: DiscoverySourceTypeFeed, EndpointType: DiscoveryEndpointFeed, Enabled: true}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	_, err := FetchDiscoverySource(context.Background(), &source)
	if !errors.Is(err, ErrFeedParse) {
		t.Fatalf("parse error = %v", err)
	}
	if strings.Contains(err.Error(), "private response excerpt") || strings.Contains(err.Error(), "rendered") {
		t.Fatalf("parse response leaked through error: %q", err.Error())
	}
	var detailer observability.FailureDetailer
	if !errors.As(err, &detailer) {
		t.Fatalf("missing failure details in %T", err)
	}
	details := detailer.ObservabilityFailure()
	if details.ErrorType != "feed_parse" || details.Domain != "example.com" || details.HTTPStatus != http.StatusOK {
		t.Fatalf("parse details = %#v", details)
	}
	if err := db.First(&source, source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if source.FailureCount != 1 || source.BackoffReason != "feed_parse" || strings.Contains(source.LastError, "rendered") {
		t.Fatalf("persisted source failure = %#v", source)
	}
}

func TestDiscoveryFetchErrorCategoryDistinguishesDNSAndTLS(t *testing.T) {
	if got := discoveryFetchErrorCategory(&net.DNSError{Err: "no such host", Name: "missing.example"}); got != "dns" {
		t.Fatalf("DNS category = %q", got)
	}
	if got := discoveryFetchErrorCategory(errors.New("remote error: tls: handshake failure")); got != "tls" {
		t.Fatalf("TLS category = %q", got)
	}
}
