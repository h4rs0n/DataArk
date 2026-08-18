package discovery

import (
	"fmt"
	"testing"

	"DataArk/config"
)

func TestScoreAndLimitCandidatesKeepsAllManualSubscriptionURLs(t *testing.T) {
	originalLimit := config.DISCOVERYMAXCANDIDATES
	config.DISCOVERYMAXCANDIDATES = 50
	t.Cleanup(func() { config.DISCOVERYMAXCANDIDATES = originalLimit })

	candidates := make([]discoveredCandidate, 0, 60)
	for i := 0; i < 60; i++ {
		candidates = append(candidates, discoveredCandidate{
			URL:   fmt.Sprintf("https://notes.example.com/posts/%d", i),
			Title: "Note",
		})
	}
	observing := scoreAndLimitCandidates(candidates, false)
	if len(observing) != 50 {
		t.Fatalf("observing cap = %d", len(observing))
	}
	manual := scoreAndLimitCandidates(candidates, true)
	if len(manual) != 60 {
		t.Fatalf("manual unlimited = %d", len(manual))
	}
}
