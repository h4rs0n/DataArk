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

func TestEstimateTokensAndNormalizeModelScores(t *testing.T) {
	if got := EstimateTokens("12345678"); got != 2 {
		t.Fatalf("ASCII tokens = %d", got)
	}
	if got := EstimateTokens("中文测试"); got != 4 {
		t.Fatalf("CJK tokens = %d", got)
	}
	got := NormalizeModelScores(Scores{Quality: 1.2, Depth: -0.1, Evergreen: 0.55})
	if got != (Scores{Quality: 1, Depth: 0, Evergreen: 0.55}) {
		t.Fatalf("normalized scores = %#v", got)
	}
}
