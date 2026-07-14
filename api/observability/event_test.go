package observability

import (
	"encoding/json"
	"strings"
	"testing"
)

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
