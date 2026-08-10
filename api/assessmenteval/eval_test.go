package assessmenteval

import (
	"DataArk/discovery"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLoadCandidateRecordsUsesImmutableCurrentContentVersion(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&discovery.DiscoveryCandidate{}, &discovery.DiscoveryArticleContentVersion{}, &discovery.DiscoveryArticleAssessment{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 7, 0, 0, 0, time.UTC)
	candidate := discovery.DiscoveryCandidate{SourceID: 1, URL: "https://fixture.example/article", CrawlHost: "fixture.example", FinalURL: "https://fixture.example/article", ContentVersion: 2, ProcessingState: discovery.DiscoveryProcessingReady, DedupeState: discovery.DiscoveryDedupeReady, LastSeenAt: now, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	oldVersion := discovery.DiscoveryArticleContentVersion{CandidateID: candidate.ID, ContentVersion: 1, ContentHash: "old-hash", Title: "Old title", BodyText: strings.Repeat("old ", 40), Language: "en", FetchedAt: now, CreatedAt: now}
	currentVersion := discovery.DiscoveryArticleContentVersion{CandidateID: candidate.ID, ContentVersion: 2, ContentHash: "current-hash", Title: "Current title", BodyText: strings.Repeat("current ", 40), Language: "en", FetchedAt: now, CreatedAt: now}
	if err := database.Create(&oldVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&currentVersion).Error; err != nil {
		t.Fatal(err)
	}
	rules := discovery.DiscoveryArticleAssessment{CandidateID: candidate.ID, ContentVersion: 2, Assessor: discovery.RuleArticleAssessorName, AssessorVersion: "2", PolicyVersion: "fixture", OverallQuality: .4, Depth: .3, EvergreenValue: .2, Reasons: "[]", CreatedAt: now}
	active := discovery.DiscoveryArticleAssessment{CandidateID: candidate.ID, ContentVersion: 2, Assessor: "model", AssessorVersion: "1", PolicyVersion: "fixture", OverallQuality: .8, Depth: .7, EvergreenValue: .6, Reasons: "[]", CreatedAt: now}
	if err := database.Create(&rules).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&candidate).Update("current_assessment_id", active.ID).Error; err != nil {
		t.Fatal(err)
	}
	records, err := LoadCandidateRecords(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ContentVersionID != currentVersion.ID || records[0].ContentHash != "current-hash" || records[0].Title != "Current title" || records[0].ActiveScores.Quality != 80 || records[0].RuleScores.Quality != 40 {
		t.Fatalf("records = %#v", records)
	}
}

func TestBuildManifestUsesFixedQuotasStressStrataAndHostCap(t *testing.T) {
	records := syntheticCandidateRecords()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	first, err := BuildManifest(records, "fixture-seed", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildManifest(records, "fixture-seed", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != GoldSampleCount || first.Digest != second.Digest {
		t.Fatalf("items=%d digest stable=%t", len(first.Items), first.Digest == second.Digest)
	}
	strata := make(map[string]int)
	hosts := make(map[string]int)
	for index, item := range first.Items {
		if item.SampleID != second.Items[index].SampleID {
			t.Fatalf("sample order changed at %d", index)
		}
		strata[item.Stratum]++
		hosts[item.Host]++
		if hosts[item.Host] > MaximumPerHost {
			t.Fatalf("host %q selected %d times", item.Host, hosts[item.Host])
		}
	}
	if core, stress := countPrefix(strata, "core:"), countPrefix(strata, "stress:"); core != 80 || stress != 40 {
		t.Fatalf("core=%d stress=%d strata=%#v", core, stress, strata)
	}
	for _, name := range []string{"stress:low-active-score", "stress:quality-boundary", "stress:saturated-high-score", "stress:model-rule-disagreement", "stress:overlong-body"} {
		if strata[name] != 8 {
			t.Fatalf("%s count=%d", name, strata[name])
		}
	}
}

func TestBlindSecondPassSelectionContainsThirtyHiddenItems(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	manifest, err := BuildManifest(syntheticCandidateRecords(), "fixture-seed", now)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := SelectPassTwoSampleIDs(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 30 {
		t.Fatalf("pass-two ids=%d", len(ids))
	}
}

func TestBuildReportComputesPerfectControlledEvaluation(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	manifest, err := BuildManifest(syntheticCandidateRecords(), "fixture-report", now.Add(-96*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Items {
		quality := (index * 7) % 101
		manifest.Items[index].BaselineScores = AxisScores{Quality: 100 - quality, Depth: 100 - ((index * 11) % 101), Evergreen: 100 - ((index * 13) % 101)}
	}
	manifest.Digest, err = ManifestDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	passOne := LabelSet{Version: LabelVersion, ManifestDigest: manifest.Digest, Pass: 1, StartedAt: now.Add(-96 * time.Hour), CompletedAt: now.Add(-90 * time.Hour)}
	for index, item := range manifest.Items {
		passOne.Labels = append(passOne.Labels, Label{SampleID: item.SampleID, Scores: fixtureScores(index), Reason: "fixture", Genre: "analysis"})
	}
	repeatedIDs, err := SelectPassTwoSampleIDs(manifest)
	if err != nil {
		t.Fatal(err)
	}
	passTwo := LabelSet{Version: LabelVersion, ManifestDigest: manifest.Digest, Pass: 2, StartedAt: now.Add(-2 * time.Hour), CompletedAt: now.Add(-time.Hour)}
	for _, id := range repeatedIDs {
		index := manifestIndex(manifest, id)
		passTwo.Labels = append(passTwo.Labels, Label{SampleID: id, Scores: fixtureScores(index), Reason: "fixture", Genre: "analysis"})
	}
	scores := ScoreSet{Version: ScoreVersion, ManifestDigest: manifest.Digest, Model: "fixture", PromptVersion: "fixture", CreatedAt: now}
	var logs strings.Builder
	for run := 1; run <= 2; run++ {
		for index, item := range manifest.Items {
			scores.Records = append(scores.Records, ScoreRecord{SampleID: item.SampleID, CandidateID: item.CandidateID, Run: run, Scores: fixtureScores(index), DurationMilliseconds: 10})
			fmt.Fprintf(&logs, `prefix dataark_event {"event":"llm_call","candidate_id":%d,"status":"success","llm_stage":"article_assessment","duration_ms":10,"llm_usage":{"available":true,"prompt_tokens":7000,"completion_tokens":40,"reasoning_tokens":0,"cached_tokens":100,"total_tokens":7040}}`+"\n", item.CandidateID)
		}
	}
	report, err := BuildReport(manifest, passOne, passTwo, nil, scores, strings.NewReader(logs.String()), now)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ActivationReady || report.ModelCore.Quality.Spearman < .999 || report.ModelRepeat.Quality.MAE != 0 || report.Protocol.PromptTokenP95 != 7000 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func syntheticCandidateRecords() []CandidateRecord {
	records := make([]CandidateRecord, 0, 180)
	nextID := uint(1)
	lengths := []int{300, 800, 3000, 10000, 21000}
	for _, quota := range coreQuotas {
		for item := 0; item < quota.count; item++ {
			bodyCharacter := "a"
			if quota.language == "zh" {
				bodyCharacter = "文"
			}
			records = append(records, candidateFixture(nextID, quota.language, strings.Repeat(bodyCharacter, lengths[quota.bucket]), 60, 60))
			nextID++
		}
	}
	appendStress := func(prefix string, quality, ruleQuality, length int) {
		for item := 0; item < 8; item++ {
			record := candidateFixture(nextID, "other", strings.Repeat(strconvDigit(item), length), quality+item%2, ruleQuality)
			record.ContentHash = fmt.Sprintf("%s-%d", prefix, nextID)
			records = append(records, record)
			nextID++
		}
	}
	appendStress("low", 5, 5, 700)
	appendStress("boundary", 49, 49, 700)
	appendStress("high", 98, 98, 700)
	appendStress("disagreement", 82, 10, 700)
	appendStress("long", 70, 70, 21000)
	return records
}

func candidateFixture(id uint, language, body string, quality, ruleQuality int) CandidateRecord {
	return CandidateRecord{
		CandidateID: id, ContentVersionID: id, ContentVersion: 1, ContentHash: fmt.Sprintf("hash-%d", id), Host: fmt.Sprintf("host-%d.example", id),
		Title: fmt.Sprintf("Article %d", id), BodyText: body, Language: language, ActiveAssessor: "baseline",
		ActiveScores: AxisScores{Quality: quality, Depth: quality, Evergreen: quality}, RuleScores: AxisScores{Quality: ruleQuality, Depth: ruleQuality, Evergreen: ruleQuality},
	}
}

func strconvDigit(index int) string { return string(rune('0' + index%10)) }

func countPrefix(values map[string]int, prefix string) int {
	var count int
	for name, value := range values {
		if strings.HasPrefix(name, prefix) {
			count += value
		}
	}
	return count
}

func fixtureScores(index int) AxisScores {
	return AxisScores{Quality: (index * 7) % 101, Depth: (index * 11) % 101, Evergreen: (index * 13) % 101}
}

func manifestIndex(manifest Manifest, sampleID string) int {
	for index, item := range manifest.Items {
		if item.SampleID == sampleID {
			return index
		}
	}
	return -1
}
