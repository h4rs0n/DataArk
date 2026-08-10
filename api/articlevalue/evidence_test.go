package articlevalue

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildEvidenceKeepsBoundedTextAndSamplesThreePositions(t *testing.T) {
	short, err := BuildEvidence("Title", "small complete body")
	if err != nil {
		t.Fatal(err)
	}
	if short.Truncated || short.BodyText != "small complete body" {
		t.Fatalf("short evidence = %#v", short)
	}

	chunks := make([]string, 0, 9000)
	for index := 0; index < 9000; index++ {
		chunks = append(chunks, fmt.Sprintf("marker-%04d", index))
	}
	long, err := BuildEvidence("Bounded", strings.Join(chunks, " "))
	if err != nil {
		t.Fatal(err)
	}
	if !long.Truncated || long.EstimatedTokens > EvidenceTokenBudget {
		t.Fatalf("long evidence budget = %#v", long)
	}
	for _, marker := range []string{"marker-0000", "marker-4500", "marker-8999", OmissionMarker} {
		if !strings.Contains(long.BodyText, marker) {
			t.Fatalf("evidence omitted %q", marker)
		}
	}
}

func TestBuildEvidenceBoundsAnAbnormallyLongTitle(t *testing.T) {
	evidence, err := BuildEvidence(strings.Repeat("超长标题", 10000), strings.Repeat("正文", 10000))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.EstimatedTokens > EvidenceTokenBudget || !strings.Contains(evidence.Title, OmissionMarker) {
		t.Fatalf("long title evidence = %#v", evidence)
	}
}

func TestEstimateTokensAndCalibrationBands(t *testing.T) {
	if got := EstimateTokens("12345678"); got != 2 {
		t.Fatalf("ASCII tokens = %d", got)
	}
	if got := EstimateTokens("中文测试"); got != 4 {
		t.Fatalf("CJK tokens = %d", got)
	}
	tests := []struct {
		tokens     int
		wantScores Scores
		confidence float64
	}{
		{79, Scores{Quality: .30, Depth: .20, Evergreen: .25}, .35},
		{80, Scores{Quality: .35, Depth: .25, Evergreen: .30}, .50},
		{200, Scores{Quality: .40, Depth: .30, Evergreen: .35}, .70},
		{400, Scores{Quality: .45, Depth: .35, Evergreen: .40}, .90},
	}
	for _, test := range tests {
		if got := FallbackScores(test.tokens); got != test.wantScores {
			t.Fatalf("fallback(%d) = %#v", test.tokens, got)
		}
		if got := EvidenceConfidence(test.tokens, false); got != test.confidence {
			t.Fatalf("confidence(%d) = %v", test.tokens, got)
		}
	}
	if got := EvidenceConfidence(400, true); got != .80 {
		t.Fatalf("truncated confidence = %v", got)
	}

	capped := ApplyEvidenceCaps(Scores{Quality: .99, Depth: .98, Evergreen: .97}, 79)
	if capped != (Scores{Quality: .40, Depth: .35, Evergreen: .50}) {
		t.Fatalf("capped scores = %#v", capped)
	}
	caps := []struct {
		tokens int
		want   Scores
	}{
		{79, Scores{Quality: .40, Depth: .35, Evergreen: .50}},
		{80, Scores{Quality: .55, Depth: .50, Evergreen: .65}},
		{200, Scores{Quality: .70, Depth: .65, Evergreen: .80}},
		{400, Scores{Quality: .99, Depth: .98, Evergreen: .97}},
	}
	for _, test := range caps {
		if got := ApplyEvidenceCaps(Scores{Quality: .99, Depth: .98, Evergreen: .97}, test.tokens); got != test.want {
			t.Fatalf("caps(%d) = %#v, want %#v", test.tokens, got, test.want)
		}
	}
}
