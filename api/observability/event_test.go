package observability

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"
)

type testFailureDetailer struct {
	err     error
	details FailureDetails
}

func (err testFailureDetailer) Error() string { return err.err.Error() }
func (err testFailureDetailer) Unwrap() error { return err.err }
func (err testFailureDetailer) ObservabilityFailure() FailureDetails {
	return err.details
}

func TestEventSchemaCannotContainSensitivePayloads(t *testing.T) {
	payload, err := json.Marshal(Event{Name: "candidate_processed", CandidateID: 7, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(payload))
	for _, forbidden := range []string{"body_text", "cookie", "authorization", "access_token", "api_key", "password"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sensitive field %q in %s", forbidden, payload)
		}
	}
}

func TestWithErrorAddsStructuredSafeDiagnostics(t *testing.T) {
	failure := testFailureDetailer{
		err:     errors.New(`GET "https://user:pass@private.example/feed?token=query-secret" failed; Authorization: Bearer header-secret; Cookie=session-secret; password=hunter2`),
		details: FailureDetails{ErrorType: "http_status", Domain: "Feed.Example.COM.", HTTPStatus: 502},
	}
	event := WithError(Event{Name: "job_fetch_source", Status: "failed"}, failure)
	if event.ErrorType != "http_status" || event.Domain != "feed.example.com" || event.HTTPStatus != 502 {
		t.Fatalf("diagnostics = %#v", event)
	}
	lower := strings.ToLower(event.ErrorMessage)
	for _, forbidden := range []string{"private.example", "/feed", "query-secret", "header-secret", "session-secret", "hunter2", "user:pass"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("unsafe value %q in %q", forbidden, event.ErrorMessage)
		}
	}
	if !strings.Contains(event.ErrorMessage, "[url]") || !strings.Contains(event.ErrorMessage, "[redacted]") {
		t.Fatalf("sanitized message = %q", event.ErrorMessage)
	}
}

func TestNormalizeEventRejectsUnsafeFieldsAndBoundsUnicodeMessage(t *testing.T) {
	event := normalizeEvent(Event{
		Domain:       "example.com/path?token=secret",
		HTTPStatus:   700,
		ErrorMessage: strings.Repeat("错", maxEventErrorRunes+10),
	})
	if event.Domain != "" || event.HTTPStatus != 0 {
		t.Fatalf("unsafe fields survived = %#v", event)
	}
	if got := len([]rune(event.ErrorMessage)); got != maxEventErrorRunes {
		t.Fatalf("message runes = %d, want %d", got, maxEventErrorRunes)
	}
}

func TestWithErrorClassifiesOrdinaryFailure(t *testing.T) {
	event := WithError(Event{Status: "failed"}, errors.New("record not found"))
	if event.ErrorType != "handler" || event.ErrorMessage != "record not found" {
		t.Fatalf("event = %#v", event)
	}
}

func TestLogSanitizesDirectFailureFields(t *testing.T) {
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	Log(Event{
		Name:         "job_fetch_source",
		Status:       "failed",
		Domain:       "example.com/private",
		HTTPStatus:   900,
		ErrorMessage: "GET https://private.example/path?token=secret failed; api_key=top-secret",
	})
	text := output.String()
	for _, forbidden := range []string{"private.example", "/path", "token=secret", "top-secret", `"http_status"`, `"domain"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unsafe value %q in %q", forbidden, text)
		}
	}
	if !strings.Contains(text, `"error_message":"GET [url] failed; api_key=[redacted]"`) {
		t.Fatalf("log output = %q", text)
	}
}
