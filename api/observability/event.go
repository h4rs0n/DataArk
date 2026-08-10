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
	Name                      string    `json:"event"`
	OccurredAt                time.Time `json:"occurred_at"`
	JobID                     string    `json:"job_id,omitempty"`
	FetchRunID                uint      `json:"fetch_run_id,omitempty"`
	SiteID                    uint      `json:"site_id,omitempty"`
	SourceID                  uint      `json:"source_id,omitempty"`
	CandidateID               uint      `json:"candidate_id,omitempty"`
	UserID                    uint      `json:"user_id,omitempty"`
	DayID                     uint      `json:"day_id,omitempty"`
	LocalDate                 string    `json:"local_date,omitempty"`
	Status                    string    `json:"status,omitempty"`
	ErrorType                 string    `json:"error_type,omitempty"`
	Domain                    string    `json:"domain,omitempty"`
	HTTPStatus                int       `json:"http_status,omitempty"`
	ErrorMessage              string    `json:"error_message,omitempty"`
	Count                     int       `json:"count,omitempty"`
	LLMStage                  string    `json:"llm_stage,omitempty"`
	LLMModel                  string    `json:"llm_model,omitempty"`
	LLMResponseMode           string    `json:"llm_response_mode,omitempty"`
	LLMAttempt                int       `json:"llm_attempt,omitempty"`
	LLMEvidenceTokens         int       `json:"llm_evidence_tokens,omitempty"`
	LLMOriginalEvidenceTokens int       `json:"llm_original_evidence_tokens,omitempty"`
	LLMEvidenceTruncated      bool      `json:"llm_evidence_truncated,omitempty"`
	LLMDuration               int64     `json:"duration_ms,omitempty"`
	LLMUsage                  *LLMUsage `json:"llm_usage,omitempty"`
}

// LLMUsage contains provider-reported counters only. It deliberately cannot
// carry prompt, completion, or reasoning text.
type LLMUsage struct {
	Available        bool `json:"available"`
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	ReasoningTokens  int  `json:"reasoning_tokens"`
	CachedTokens     int  `json:"cached_tokens"`
	TotalTokens      int  `json:"total_tokens"`
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
	event.LLMStage = boundedEventValue(event.LLMStage, 64)
	event.LLMModel = boundedEventValue(event.LLMModel, 255)
	event.LLMResponseMode = boundedEventValue(event.LLMResponseMode, 32)
	event.LLMAttempt = nonNegativeCount(event.LLMAttempt)
	event.LLMEvidenceTokens = nonNegativeCount(event.LLMEvidenceTokens)
	event.LLMOriginalEvidenceTokens = nonNegativeCount(event.LLMOriginalEvidenceTokens)
	if event.LLMDuration < 0 {
		event.LLMDuration = 0
	}
	if event.LLMUsage != nil {
		usage := *event.LLMUsage
		usage.PromptTokens = nonNegativeCount(usage.PromptTokens)
		usage.CompletionTokens = nonNegativeCount(usage.CompletionTokens)
		usage.ReasoningTokens = nonNegativeCount(usage.ReasoningTokens)
		usage.CachedTokens = nonNegativeCount(usage.CachedTokens)
		usage.TotalTokens = nonNegativeCount(usage.TotalTokens)
		event.LLMUsage = &usage
	}
	event.Domain = normalizeDomain(event.Domain)
	if event.HTTPStatus < 100 || event.HTTPStatus > 599 {
		event.HTTPStatus = 0
	}
	event.ErrorMessage = compactEventError(event.ErrorMessage)
	return event
}

func boundedEventValue(value string, maximum int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if maximum > 0 && len(runes) > maximum {
		return string(runes[:maximum])
	}
	return value
}

func nonNegativeCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
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
