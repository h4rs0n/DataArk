package discovery

import (
	"DataArk/observability"
	"context"
	"errors"
	"fmt"
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
