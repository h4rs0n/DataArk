package assessmenteval

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

const (
	CoreSampleCount   = 80
	StressSampleCount = 40
	GoldSampleCount   = CoreSampleCount + StressSampleCount
	MaximumPerHost    = 2
)

type coreQuota struct {
	language string
	bucket   int
	count    int
}

var coreQuotas = []coreQuota{
	{"zh", 0, 11}, {"zh", 1, 18}, {"zh", 2, 14}, {"zh", 3, 4}, {"zh", 4, 2},
	{"en", 0, 5}, {"en", 1, 10}, {"en", 2, 11}, {"en", 3, 4}, {"en", 4, 1},
}

var bodyCharacterBuckets = []int{500, 2000, 8000, 20000}

type databaseCandidateRow struct {
	CandidateID      uint
	ContentVersionID uint
	ContentVersion   uint
	ContentHash      string
	CrawlHost        string
	FinalURL         string
	Title            string
	BodyText         string
	Language         string
	ActiveAssessor   string
	ActiveQuality    float64
	ActiveDepth      float64
	ActiveEvergreen  float64
	RuleQuality      float64
	RuleDepth        float64
	RuleEvergreen    float64
}

func LoadCandidateRecords(database *gorm.DB) ([]CandidateRecord, error) {
	if database == nil {
		return nil, errors.New("assessment sample database is unavailable")
	}
	var rows []databaseCandidateRow
	err := database.Raw(`
SELECT c.id AS candidate_id,
       cv.id AS content_version_id,
       cv.content_version,
       cv.content_hash,
       c.crawl_host,
       COALESCE(NULLIF(cv.canonical_url, ''), NULLIF(cv.final_url, ''), c.final_url, c.url) AS final_url,
       cv.title,
       cv.body_text,
       cv.language,
       COALESCE(active.assessor, '') AS active_assessor,
       COALESCE(active.overall_quality, 0) AS active_quality,
       COALESCE(active.depth, 0) AS active_depth,
       COALESCE(active.evergreen_value, 0) AS active_evergreen,
       COALESCE((SELECT rules.overall_quality FROM discovery_article_assessments rules
                 WHERE rules.candidate_id = c.id AND rules.content_version = c.content_version
                   AND rules.assessor = 'deterministic_rules' ORDER BY rules.id DESC LIMIT 1), 0) AS rule_quality,
       COALESCE((SELECT rules.depth FROM discovery_article_assessments rules
                 WHERE rules.candidate_id = c.id AND rules.content_version = c.content_version
                   AND rules.assessor = 'deterministic_rules' ORDER BY rules.id DESC LIMIT 1), 0) AS rule_depth,
       COALESCE((SELECT rules.evergreen_value FROM discovery_article_assessments rules
                 WHERE rules.candidate_id = c.id AND rules.content_version = c.content_version
                   AND rules.assessor = 'deterministic_rules' ORDER BY rules.id DESC LIMIT 1), 0) AS rule_evergreen
FROM discovery_candidates c
JOIN discovery_article_content_versions cv
  ON cv.candidate_id = c.id AND cv.content_version = c.content_version
LEFT JOIN discovery_article_assessments active ON active.id = c.current_assessment_id
WHERE c.processing_state = 'ready'
  AND c.dedupe_state = 'ready'
  AND (c.representative_id IS NULL OR c.representative_id = c.id)
  AND c.current_assessment_id IS NOT NULL
  AND LENGTH(TRIM(cv.body_text)) >= 120`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	records := make([]CandidateRecord, 0, len(rows))
	for _, row := range rows {
		host := strings.ToLower(strings.TrimSpace(row.CrawlHost))
		if host == "" {
			if parsed, parseErr := url.Parse(row.FinalURL); parseErr == nil {
				host = strings.ToLower(parsed.Hostname())
			}
		}
		records = append(records, CandidateRecord{
			CandidateID: row.CandidateID, ContentVersionID: row.ContentVersionID, ContentVersion: row.ContentVersion,
			ContentHash: row.ContentHash, Host: host, Title: row.Title, BodyText: row.BodyText, Language: row.Language,
			ActiveAssessor: row.ActiveAssessor,
			ActiveScores:   AxisScores{Quality: scoreFromFraction(row.ActiveQuality), Depth: scoreFromFraction(row.ActiveDepth), Evergreen: scoreFromFraction(row.ActiveEvergreen)},
			RuleScores:     AxisScores{Quality: scoreFromFraction(row.RuleQuality), Depth: scoreFromFraction(row.RuleDepth), Evergreen: scoreFromFraction(row.RuleEvergreen)},
		})
	}
	return records, nil
}

func BuildManifest(records []CandidateRecord, seed string, createdAt time.Time) (Manifest, error) {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return Manifest{}, errors.New("assessment sample seed is required")
	}
	unique := make([]CandidateRecord, 0, len(records))
	seenContent := make(map[string]struct{})
	for _, record := range records {
		record.Title = strings.TrimSpace(record.Title)
		record.BodyText = strings.TrimSpace(record.BodyText)
		if record.CandidateID == 0 || record.ContentVersion == 0 || record.ContentHash == "" || record.BodyText == "" {
			continue
		}
		if _, exists := seenContent[record.ContentHash]; exists {
			continue
		}
		seenContent[record.ContentHash] = struct{}{}
		unique = append(unique, record)
	}
	selected := make([]ManifestItem, 0, GoldSampleCount)
	chosen := make(map[string]struct{}, GoldSampleCount)
	hostCounts := make(map[string]int)
	for _, quota := range coreQuotas {
		eligible := filterRecords(unique, func(record CandidateRecord) bool {
			return normalizeLanguage(record.Language, record.BodyText) == quota.language && characterBucket(len([]rune(record.BodyText))) == quota.bucket
		})
		sortRecords(eligible, seed, fmt.Sprintf("core:%s:%d", quota.language, quota.bucket), nil)
		items := takeRecords(eligible, quota.count, chosen, hostCounts, fmt.Sprintf("core:%s:%s", quota.language, characterBucketName(quota.bucket)))
		if len(items) != quota.count {
			return Manifest{}, fmt.Errorf("insufficient core samples for %s bucket %s: got %d want %d", quota.language, characterBucketName(quota.bucket), len(items), quota.count)
		}
		selected = append(selected, items...)
	}
	stressSelectors := []struct {
		name   string
		filter func(CandidateRecord) bool
		value  func(CandidateRecord) float64
		desc   bool
	}{
		{name: "low-active-score", filter: func(CandidateRecord) bool { return true }, value: func(record CandidateRecord) float64 { return float64(record.ActiveScores.Quality) }},
		{name: "quality-boundary", filter: func(record CandidateRecord) bool {
			return record.ActiveScores.Quality >= 45 && record.ActiveScores.Quality <= 55
		}, value: func(record CandidateRecord) float64 { return math.Abs(float64(record.ActiveScores.Quality - 50)) }},
		{name: "saturated-high-score", filter: func(record CandidateRecord) bool { return record.ActiveScores.Quality >= 95 }, value: func(record CandidateRecord) float64 { return float64(record.ActiveScores.Quality) }, desc: true},
		{name: "model-rule-disagreement", filter: func(CandidateRecord) bool { return true }, value: func(record CandidateRecord) float64 {
			return math.Abs(float64(record.ActiveScores.Quality - record.RuleScores.Quality))
		}, desc: true},
		{name: "overlong-body", filter: func(record CandidateRecord) bool { return len([]rune(record.BodyText)) >= 20000 }, value: func(record CandidateRecord) float64 { return float64(len([]rune(record.BodyText))) }, desc: true},
	}
	for _, selector := range stressSelectors {
		eligible := filterRecords(unique, selector.filter)
		sortRecords(eligible, seed, "stress:"+selector.name, func(left, right CandidateRecord) bool {
			leftValue, rightValue := selector.value(left), selector.value(right)
			if leftValue == rightValue {
				return false
			}
			if selector.desc {
				return leftValue > rightValue
			}
			return leftValue < rightValue
		})
		items := takeRecords(eligible, 8, chosen, hostCounts, "stress:"+selector.name)
		if len(items) != 8 {
			return Manifest{}, fmt.Errorf("insufficient stress samples for %s: got %d want 8", selector.name, len(items))
		}
		selected = append(selected, items...)
	}
	manifest := Manifest{Version: ManifestVersion, Seed: seed, CreatedAt: createdAt.UTC(), Items: selected}
	digest, err := ManifestDigest(manifest)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Digest = digest
	return manifest, nil
}

func takeRecords(records []CandidateRecord, count int, chosen map[string]struct{}, hostCounts map[string]int, stratum string) []ManifestItem {
	items := make([]ManifestItem, 0, count)
	for _, record := range records {
		if len(items) >= count {
			break
		}
		if _, exists := chosen[record.ContentHash]; exists {
			continue
		}
		host := record.Host
		if host == "" {
			host = "unknown:" + record.ContentHash
		}
		if hostCounts[host] >= MaximumPerHost {
			continue
		}
		chosen[record.ContentHash] = struct{}{}
		hostCounts[host]++
		items = append(items, ManifestItem{
			SampleID: sampleID(record), CandidateID: record.CandidateID, ContentVersionID: record.ContentVersionID,
			ContentVersion: record.ContentVersion, ContentHash: record.ContentHash, Title: record.Title, BodyText: record.BodyText,
			Language: normalizeLanguage(record.Language, record.BodyText), BodyCharacters: len([]rune(record.BodyText)), Host: record.Host,
			Stratum: stratum, BaselineAssessor: record.ActiveAssessor, BaselineScores: record.ActiveScores, RuleScores: record.RuleScores,
		})
	}
	return items
}

func filterRecords(records []CandidateRecord, predicate func(CandidateRecord) bool) []CandidateRecord {
	filtered := make([]CandidateRecord, 0, len(records))
	for _, record := range records {
		if predicate(record) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func sortRecords(records []CandidateRecord, seed, stratum string, preferred func(CandidateRecord, CandidateRecord) bool) {
	sort.Slice(records, func(left, right int) bool {
		if preferred != nil {
			if preferred(records[left], records[right]) {
				return true
			}
			if preferred(records[right], records[left]) {
				return false
			}
		}
		return stableKey(seed, stratum, records[left]) < stableKey(seed, stratum, records[right])
	})
}

func stableKey(seed, stratum string, record CandidateRecord) string {
	digest := sha256.Sum256([]byte(seed + "\x00" + stratum + "\x00" + record.ContentHash + "\x00" + fmt.Sprint(record.CandidateID, ":", record.ContentVersion)))
	return hex.EncodeToString(digest[:])
}

func sampleID(record CandidateRecord) string {
	digest := sha256.Sum256([]byte(record.ContentHash + "\x00" + fmt.Sprint(record.CandidateID, ":", record.ContentVersion)))
	return hex.EncodeToString(digest[:12])
}

func characterBucket(characters int) int {
	for index, upper := range bodyCharacterBuckets {
		if characters < upper {
			return index
		}
	}
	return len(bodyCharacterBuckets)
}

func characterBucketName(bucket int) string {
	return []string{"120-499", "500-1999", "2000-7999", "8000-19999", "20000+"}[bucket]
}

func normalizeLanguage(language, body string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if strings.HasPrefix(language, "zh") || strings.Contains(language, "chinese") {
		return "zh"
	}
	if strings.HasPrefix(language, "en") || strings.Contains(language, "english") {
		return "en"
	}
	var letters, han int
	for _, character := range body {
		if unicode.Is(unicode.Han, character) {
			han++
		}
		if unicode.IsLetter(character) {
			letters++
		}
	}
	if letters > 0 && float64(han)/float64(letters) >= .20 {
		return "zh"
	}
	if letters > 0 {
		return "en"
	}
	return "other"
}

func scoreFromFraction(value float64) int {
	return clampScore(int(math.Round(value * 100)))
}

func clampScore(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
