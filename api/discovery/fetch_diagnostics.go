package discovery

import (
	"DataArk/observability"
	"errors"
	"strings"
)

type fetchDiagnosticError struct {
	cause      error
	errorType  string
	domain     string
	httpStatus int
}

func (err *fetchDiagnosticError) Error() string {
	if err == nil || err.cause == nil {
		return ""
	}
	return err.cause.Error()
}

func (err *fetchDiagnosticError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (err *fetchDiagnosticError) ObservabilityFailure() observability.FailureDetails {
	if err == nil {
		return observability.FailureDetails{}
	}
	return observability.FailureDetails{
		ErrorType:  err.errorType,
		Domain:     err.domain,
		HTTPStatus: err.httpStatus,
	}
}

func withFetchDiagnostic(cause error, rawURL string, httpStatus int, errorType string) error {
	if cause == nil {
		return nil
	}
	details := observability.FailureDetails{}
	var detailer observability.FailureDetailer
	if errors.As(cause, &detailer) {
		details = detailer.ObservabilityFailure()
	}
	domain := crawlHostForURL(rawURL)
	if domain == "" {
		domain = details.Domain
	}
	if httpStatus == 0 {
		httpStatus = details.HTTPStatus
	}
	if strings.TrimSpace(errorType) == "" {
		errorType = details.ErrorType
	}
	return &fetchDiagnosticError{cause: cause, errorType: errorType, domain: domain, httpStatus: httpStatus}
}

func withHTTPStatusDiagnostic(cause error, fetched FetchResult, fallbackURL string) error {
	rawURL := strings.TrimSpace(fetched.FinalURL)
	if rawURL == "" {
		rawURL = fallbackURL
	}
	return withFetchDiagnostic(cause, rawURL, fetched.StatusCode, "http_status")
}
