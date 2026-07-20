package observability

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"regexp"
	"strings"
	"time"
)

const maxEventErrorRunes = 300

var (
	eventURLPattern    = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
	eventSecretPattern = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|cookie|set-cookie|token|access[_-]?token|api[_-]?key|password|passwd|secret|client[_-]?secret)\b\s*[:=]\s*[^,;]+`)
	eventBearerPattern = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
)

// Event has a fixed operational schema. Its failure diagnostics are normalized
// before logging and cannot carry complete URLs or unbounded payloads.
type Event struct {
	Name         string    `json:"event"`
	OccurredAt   time.Time `json:"occurred_at"`
	JobID        string    `json:"job_id,omitempty"`
	FetchRunID   uint      `json:"fetch_run_id,omitempty"`
	SiteID       uint      `json:"site_id,omitempty"`
	SourceID     uint      `json:"source_id,omitempty"`
	CandidateID  uint      `json:"candidate_id,omitempty"`
	UserID       uint      `json:"user_id,omitempty"`
	DayID        uint      `json:"day_id,omitempty"`
	LocalDate    string    `json:"local_date,omitempty"`
	Status       string    `json:"status,omitempty"`
	ErrorType    string    `json:"error_type,omitempty"`
	Domain       string    `json:"domain,omitempty"`
	HTTPStatus   int       `json:"http_status,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	Count        int       `json:"count,omitempty"`
}

// FailureDetails is the fixed, safe metadata an error may expose to structured
// events. Domain is a hostname only, never a complete URL.
type FailureDetails struct {
	ErrorType  string
	Domain     string
	HTTPStatus int
}

// FailureDetailer lets domain packages attach safe operational metadata while
// preserving their original error and errors.Is/errors.As behavior.
type FailureDetailer interface {
	ObservabilityFailure() FailureDetails
}

// WithError enriches an event with bounded diagnostics for a failed operation.
func WithError(event Event, err error) Event {
	if err == nil {
		return normalizeEvent(event)
	}
	event.ErrorType = "handler"
	var detailer FailureDetailer
	if errors.As(err, &detailer) {
		details := detailer.ObservabilityFailure()
		if strings.TrimSpace(details.ErrorType) != "" {
			event.ErrorType = details.ErrorType
		}
		event.Domain = details.Domain
		event.HTTPStatus = details.HTTPStatus
	} else if errors.Is(err, context.DeadlineExceeded) {
		event.ErrorType = "timeout"
	} else {
		var networkError net.Error
		if errors.As(err, &networkError) {
			if networkError.Timeout() {
				event.ErrorType = "timeout"
			} else {
				event.ErrorType = "network"
			}
		}
	}
	event.ErrorMessage = err.Error()
	return normalizeEvent(event)
}

func Log(event Event) {
	event = normalizeEvent(event)
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	log.Printf("dataark_event %s", payload)
}

func normalizeEvent(event Event) Event {
	event.ErrorType = strings.TrimSpace(event.ErrorType)
	event.Domain = normalizeDomain(event.Domain)
	if event.HTTPStatus < 100 || event.HTTPStatus > 599 {
		event.HTTPStatus = 0
	}
	event.ErrorMessage = compactEventError(event.ErrorMessage)
	return event
}

func normalizeDomain(value string) string {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "/\\?#@") {
		return ""
	}
	if net.ParseIP(value) != nil {
		return value
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return ""
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return ""
			}
		}
	}
	return value
}

func compactEventError(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = eventURLPattern.ReplaceAllString(value, "[url]")
	value = eventBearerPattern.ReplaceAllString(value, "Bearer [redacted]")
	value = eventSecretPattern.ReplaceAllString(value, "$1=[redacted]")
	runes := []rune(value)
	if len(runes) > maxEventErrorRunes {
		return string(runes[:maxEventErrorRunes])
	}
	return value
}
