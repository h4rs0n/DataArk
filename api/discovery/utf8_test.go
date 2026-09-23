package discovery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTruncateValidUTF8Bytes(t *testing.T) {
	tests := []struct {
		name  string
		value string
		limit int
		want  string
	}{
		{"ascii", "abcdef", 4, "abcd"},
		{"exact rune boundary", "你好世界", 6, "你好"},
		{"inside chinese rune", "你好世界", 5, "你"},
		{"inside emoji", "a😀b", 4, "a"},
		{"too small for rune", "你", 2, ""},
		{"invalid source", "a\xffb", 5, "a�b"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := truncateValidUTF8Bytes(testCase.value, testCase.limit)
			if got != testCase.want || len(got) > testCase.limit || !utf8.ValidString(got) {
				t.Fatalf("truncateValidUTF8Bytes(%q, %d) = %q", testCase.value, testCase.limit, got)
			}
		})
	}
}

func TestGraphEvidenceAndOperationalErrorStayValidUTF8(t *testing.T) {
	anchor := strings.Repeat("中", 90)
	evidence := compactGraphEvidence(BlogrollLink{DetectionRule: "rulex", AnchorText: anchor})
	if len(evidence) > 240 || !utf8.ValidString(evidence) || strings.ContainsRune(evidence, utf8.RuneError) {
		t.Fatalf("invalid evidence: %q", evidence)
	}
	detail := truncateOperationalError(strings.Repeat("错误", 90))
	if len(detail) > 512 || !utf8.ValidString(detail) || strings.ContainsRune(detail, utf8.RuneError) {
		t.Fatalf("invalid operational detail: %q", detail)
	}

	setupSQLiteDB(t)
	now := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	from := DiscoverySite{RootURL: "https://source.example/", HostKey: "source.example", DomainKey: "source.example", Status: DiscoverySiteStatusSeed, DiscoveryMethod: DiscoveryMethodManualSeed, CrawlAllowed: true, FirstDiscoveredAt: now}
	if err := db.Create(&from).Error; err != nil {
		t.Fatal(err)
	}
	link := BlogrollLink{TargetURL: "https://target.example/", SourcePageURL: from.RootURL, AnchorText: anchor, DetectionRule: "rulex", RelationType: "friend", Confidence: 0.9}
	if _, err := (SiteGraphService{Clock: &advancingClock{now: now}}).ApplyLinks(context.Background(), from, []BlogrollLink{link}); err != nil {
		t.Fatal(err)
	}
	var edge DiscoverySiteEdge
	if err := db.First(&edge).Error; err != nil {
		t.Fatal(err)
	}
	if edge.EvidenceSummary != evidence || !utf8.ValidString(edge.EvidenceSummary) {
		t.Fatalf("stored evidence: %q", edge.EvidenceSummary)
	}
	if err := db.Model(&from).Update("operational_details", detail).Error; err != nil {
		t.Fatal(err)
	}
	var stored DiscoverySite
	if err := db.First(&stored, from.ID).Error; err != nil || stored.OperationalDetails != detail {
		t.Fatalf("stored operational detail: %q, err=%v", stored.OperationalDetails, err)
	}
}

func TestGraphTargetLockKeysAreSortedIndependentlyOfConfidence(t *testing.T) {
	from := DiscoverySite{DomainKey: "source.example"}
	links := []BlogrollLink{
		{TargetURL: "https://z.example/", Confidence: 0.9},
		{TargetURL: "https://a.example/", Confidence: 0.1},
		{TargetURL: "https://source.example/"},
		{TargetURL: "not a URL"},
	}
	keys := graphTargetLockKeys(links, from)
	if len(keys) != 2 || keys[0] != "a.example" || keys[1] != "z.example" {
		t.Fatalf("lock keys = %#v", keys)
	}
	ordered := dedupeBlogrollLinks(links)
	if len(ordered) < 2 || ordered[0].TargetURL != "https://z.example/" {
		t.Fatalf("confidence order changed: %#v", ordered)
	}
}

type testGraphSQLError struct{ state string }

func (err testGraphSQLError) Error() string    { return err.state }
func (err testGraphSQLError) SQLState() string { return err.state }

func TestGraphDeadlockDetection(t *testing.T) {
	if !isGraphDeadlock(errors.Join(errors.New("graph update"), testGraphSQLError{state: "40P01"})) {
		t.Fatal("wrapped deadlock not detected")
	}
	if isGraphDeadlock(testGraphSQLError{state: "22021"}) || isGraphDeadlock(errors.New("40P01")) {
		t.Fatal("non-deadlock classified as deadlock")
	}
}
