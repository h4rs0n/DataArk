package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DiscoveryDedupeReady = "ready"

	DuplicateMatchUnique    = "unique"
	DuplicateMatchURL       = "url"
	DuplicateMatchCanonical = "canonical"
	DuplicateMatchExact     = "exact_body"
	DuplicateMatchNear      = "near_body"
)

type candidateDuplicateMatch struct {
	Candidate  DiscoveryCandidate
	Method     string
	Similarity float64
}

// ResolveCandidateDuplicates assigns a deterministic cluster and one
// recommendable representative without deleting any member or content version.
func ResolveCandidateDuplicates(ctx context.Context, candidateID uint) error {
	if db == nil || candidateID == 0 {
		return gorm.ErrRecordNotFound
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var target DiscoveryCandidate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, candidateID).Error; err != nil {
			return err
		}
		if target.ProcessingState != DiscoveryProcessingReady || strings.TrimSpace(target.ContentHash) == "" {
			return nil
		}

		matches, strongest, maximumSimilarity, err := duplicateMatches(tx, target)
		if err != nil {
			return err
		}
		membersByID := map[uint]DiscoveryCandidate{target.ID: target}
		for _, match := range matches {
			membersByID[match.Candidate.ID] = match.Candidate
		}
		clusterIDs := make([]string, 0)
		for _, member := range membersByID {
			if clusterID := strings.TrimSpace(member.DuplicateClusterID); clusterID != "" {
				clusterIDs = append(clusterIDs, clusterID)
			}
		}
		clusterIDs = uniqueSortedStrings(clusterIDs)
		if len(clusterIDs) > 0 {
			var clustered []DiscoveryCandidate
			if err := tx.Where("duplicate_cluster_id IN ?", clusterIDs).Find(&clustered).Error; err != nil {
				return err
			}
			for _, member := range clustered {
				membersByID[member.ID] = member
			}
		}
		members := make([]DiscoveryCandidate, 0, len(membersByID))
		for _, member := range membersByID {
			members = append(members, member)
		}
		sort.Slice(members, func(i, j int) bool { return members[i].ID < members[j].ID })
		if len(members) == 1 {
			strongest = DuplicateMatchUnique
			maximumSimilarity = 1
		}
		clusterID := chooseDuplicateClusterID(clusterIDs, members)
		representative, reason := chooseDuplicateRepresentative(members)
		now := discoveryClock.Now()

		for _, member := range members {
			if err := saveCandidateIdentities(tx, member, now); err != nil {
				return err
			}
			updates := map[string]interface{}{
				"duplicate_cluster_id": clusterID,
				"representative_id":    representative.ID,
				"dedupe_key":           clusterID,
				"dedupe_state":         DiscoveryDedupeReady,
				"updated_at":           now,
			}
			if member.ID == representative.ID {
				if member.AssessmentState != "ready" {
					updates["eligibility_state"] = DiscoveryEligibilityUnknown
					updates["eligibility_reasons"] = "assessment_pending"
				}
			} else {
				updates["eligibility_state"] = DiscoveryEligibilityIneligible
				updates["eligibility_reasons"] = "duplicate_non_representative"
			}
			if err := tx.Model(&DiscoveryCandidate{}).Where("id = ?", member.ID).Updates(updates).Error; err != nil {
				return err
			}
		}

		nonRepresentativeIDs := make([]uint, 0, len(members)-1)
		for _, member := range members {
			if member.ID != representative.ID {
				nonRepresentativeIDs = append(nonRepresentativeIDs, member.ID)
			}
		}
		if len(nonRepresentativeIDs) > 0 {
			if err := tx.Model(&DiscoveryCandidateProvenance{}).
				Where("candidate_id IN ?", nonRepresentativeIDs).
				Updates(map[string]interface{}{"candidate_id": representative.ID, "updated_at": now}).Error; err != nil {
				return err
			}
		}

		cluster := DiscoveryDuplicateCluster{
			ClusterID: clusterID, RepresentativeID: representative.ID, MatchMethod: strongest,
			RepresentativeReason: fmt.Sprintf("%s; max_similarity=%.3f", reason, maximumSimilarity),
			MemberCount:          uint(len(members)), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "cluster_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"representative_id": representative.ID, "match_method": strongest,
				"representative_reason": cluster.RepresentativeReason,
				"member_count":          uint(len(members)), "updated_at": now,
			}),
		}).Create(&cluster).Error; err != nil {
			return err
		}
		if len(clusterIDs) > 0 {
			obsolete := make([]string, 0)
			for _, existing := range clusterIDs {
				if existing != clusterID {
					obsolete = append(obsolete, existing)
				}
			}
			if len(obsolete) > 0 {
				if err := tx.Where("cluster_id IN ?", obsolete).Delete(&DiscoveryDuplicateCluster{}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func duplicateMatches(tx *gorm.DB, target DiscoveryCandidate) ([]candidateDuplicateMatch, string, float64, error) {
	byID := make(map[uint]candidateDuplicateMatch)
	strongest := DuplicateMatchUnique
	maximumSimilarity := 0.0
	var exactCandidates []DiscoveryCandidate
	urlValues := candidateURLValues(target)
	query := tx.Where("id <> ? AND processing_state = ?", target.ID, DiscoveryProcessingReady).
		Where("content_hash = ? OR canonical_url IN ? OR final_url IN ? OR normalized_url IN ? OR url IN ?", target.ContentHash, urlValues, urlValues, urlValues, urlValues)
	if err := query.Find(&exactCandidates).Error; err != nil {
		return nil, strongest, maximumSimilarity, err
	}
	for _, candidate := range exactCandidates {
		method := duplicateMatchMethod(target, candidate)
		similarity := 1.0
		byID[candidate.ID] = candidateDuplicateMatch{Candidate: candidate, Method: method, Similarity: similarity}
		if duplicateMethodRank(method) > duplicateMethodRank(strongest) {
			strongest = method
		}
		maximumSimilarity = 1
	}

	minimumWords := target.WordCount / 2
	maximumWords := target.WordCount * 2
	if minimumWords < 20 {
		minimumWords = 20
	}
	if maximumWords < 40 {
		maximumWords = 40
	}
	var nearCandidates []DiscoveryCandidate
	if target.WordCount >= 20 {
		if err := tx.Where("id <> ? AND processing_state = ? AND language = ? AND word_count BETWEEN ? AND ?", target.ID, DiscoveryProcessingReady, target.Language, minimumWords, maximumWords).
			Order("id").Limit(500).Find(&nearCandidates).Error; err != nil {
			return nil, strongest, maximumSimilarity, err
		}
	}
	for _, candidate := range nearCandidates {
		if _, exists := byID[candidate.ID]; exists {
			continue
		}
		similarity := articleTextSimilarity(target, candidate)
		if similarity < 0.74 {
			continue
		}
		byID[candidate.ID] = candidateDuplicateMatch{Candidate: candidate, Method: DuplicateMatchNear, Similarity: similarity}
		if duplicateMethodRank(DuplicateMatchNear) > duplicateMethodRank(strongest) {
			strongest = DuplicateMatchNear
		}
		if similarity > maximumSimilarity {
			maximumSimilarity = similarity
		}
	}
	matches := make([]candidateDuplicateMatch, 0, len(byID))
	for _, match := range byID {
		matches = append(matches, match)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Candidate.ID < matches[j].Candidate.ID })
	return matches, strongest, maximumSimilarity, nil
}

func duplicateMatchMethod(first DiscoveryCandidate, second DiscoveryCandidate) string {
	if first.ContentHash != "" && first.ContentHash == second.ContentHash {
		return DuplicateMatchExact
	}
	if first.CanonicalURL != "" && first.CanonicalURL == second.CanonicalURL {
		return DuplicateMatchCanonical
	}
	return DuplicateMatchURL
}

func duplicateMethodRank(method string) int {
	switch method {
	case DuplicateMatchExact:
		return 4
	case DuplicateMatchCanonical:
		return 3
	case DuplicateMatchURL:
		return 2
	case DuplicateMatchNear:
		return 1
	default:
		return 0
	}
}

func candidateURLValues(candidate DiscoveryCandidate) []string {
	values := []string{candidate.URL, candidate.NormalizedURL, candidate.FinalURL, candidate.CanonicalURL}
	for _, value := range append([]string(nil), values...) {
		if normalized, err := NormalizeArticleURL(value); err == nil {
			values = append(values, normalized)
		}
	}
	return uniqueSortedStrings(values)
}

func chooseDuplicateClusterID(existing []string, members []DiscoveryCandidate) string {
	if len(existing) > 0 {
		return existing[0]
	}
	identities := make([]string, 0, len(members)*2)
	for _, member := range members {
		identities = append(identities, member.ContentHash, member.CanonicalURL)
	}
	identities = uniqueSortedStrings(identities)
	if len(identities) == 0 {
		identities = append(identities, fmt.Sprintf("candidate:%d", members[0].ID))
	}
	sum := sha256.Sum256([]byte(strings.Join(identities, "\x00")))
	return "dup-" + hex.EncodeToString(sum[:16])
}

func chooseDuplicateRepresentative(members []DiscoveryCandidate) (DiscoveryCandidate, string) {
	representative := members[0]
	bestScore := duplicateRepresentativeScore(representative)
	for _, member := range members[1:] {
		score := duplicateRepresentativeScore(member)
		if score > bestScore || (score == bestScore && member.ID < representative.ID) {
			representative = member
			bestScore = score
		}
	}
	return representative, fmt.Sprintf("representative=%d score=%d based_on=accessibility,canonical_origin,final_url,body_completeness,published_metadata", representative.ID, bestScore)
}

func duplicateRepresentativeScore(candidate DiscoveryCandidate) int {
	score := 0
	if candidate.ProcessingState == DiscoveryProcessingReady {
		score += 1000
	}
	canonicalHost := articleHost(candidate.CanonicalURL)
	if canonicalHost != "" && canonicalHost == articleHost(candidate.URL) {
		score += 300
	}
	if candidate.CanonicalURL != "" && normalizedURLsEqual(candidate.FinalURL, candidate.CanonicalURL) {
		score += 150
	}
	if candidate.CanonicalURL != "" {
		score += 50
	}
	words := candidate.WordCount
	if words <= 0 {
		words = len(strings.Fields(candidate.BodyText))
	}
	if words > 500 {
		words = 500
	}
	score += words
	if candidate.PublishedAt != nil && candidate.PublishedConfidence != "discovered_at" {
		score += 25
	}
	return score
}

func normalizedURLsEqual(first string, second string) bool {
	firstNormalized, firstErr := NormalizeArticleURL(first)
	secondNormalized, secondErr := NormalizeArticleURL(second)
	return firstErr == nil && secondErr == nil && firstNormalized == secondNormalized
}

func articleHost(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func saveCandidateIdentities(tx *gorm.DB, candidate DiscoveryCandidate, now time.Time) error {
	values := map[string][]string{
		"raw_url":      {candidate.URL},
		"normalized":   {candidate.NormalizedURL},
		"final_url":    {candidate.FinalURL},
		"canonical":    {candidate.CanonicalURL},
		"content_hash": {candidate.ContentHash},
	}
	for kind, identities := range values {
		for _, value := range uniqueSortedStrings(identities) {
			sum := sha256.Sum256([]byte(kind + "\x00" + value))
			identity := DiscoveryCandidateIdentity{
				CandidateID: candidate.ID, Kind: kind, IdentityKey: hex.EncodeToString(sum[:]),
				Value: value, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&identity).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func articleTextSimilarity(first DiscoveryCandidate, second DiscoveryCandidate) float64 {
	firstShingles := textShingles(first.Title+" "+first.BodyText, 3)
	secondShingles := textShingles(second.Title+" "+second.BodyText, 3)
	if len(firstShingles) == 0 || len(secondShingles) == 0 {
		return 0
	}
	intersection := 0
	for shingle := range firstShingles {
		if _, ok := secondShingles[shingle]; ok {
			intersection++
		}
	}
	union := len(firstShingles) + len(secondShingles) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func textShingles(value string, size int) map[string]struct{} {
	words := strings.FieldsFunc(strings.ToLower(value), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsNumber(character)
	})
	if len(words) < size {
		return map[string]struct{}{}
	}
	result := make(map[string]struct{}, len(words)-size+1)
	for index := 0; index+size <= len(words); index++ {
		result[strings.Join(words[index:index+size], " ")] = struct{}{}
	}
	return result
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
