package discovery

import (
	"DataArk/articlevalue"
	"DataArk/config"
	"DataArk/dedupe"
	"DataArk/jobqueue"
	"DataArk/material"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const dedupeBatchSize = 250

// Phases that write candidate projections hold a material state row lock per
// group until commit. Their batches stay small so assessment activation for an
// affected material waits milliseconds rather than for a whole 250-group batch.
const dedupeWriteBatchSize = 25

var ErrDedupeConflict = errors.New("dedupe run state conflicts with requested action")

type dedupeArticle struct {
	RepresentationID uint
	MaterialID       uint
	VersionID        uint
	ContentVersion   uint
	CandidateID      uint
	Text             string
	ContentHash      string
}

func dedupeArticles(database *gorm.DB) *gorm.DB {
	return database.Table("material_representations r").
		Select(`r.id AS representation_id, m.id AS material_id, r.version_id, v.version AS content_version,
  r.text, r.content_hash, COALESCE((SELECT MIN(c.id) FROM discovery_candidates c
  WHERE c.material_id=m.id AND c.processing_state='ready'),0) AS candidate_id`).
		Joins("JOIN material m ON m.current_version_id=r.version_id").
		Joins("JOIN material_versions v ON v.id=r.version_id").
		Joins("JOIN material_article_states s ON s.material_id=m.id").
		Where("r.kind = ? AND r.role = ? AND r.text <> ''", "text", "body").
		Where("NOT EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=m.id) OR EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=m.id AND c.processing_state='ready')")
}
func loadDedupeArticle(database *gorm.DB, representationID uint) (dedupeArticle, error) {
	var article dedupeArticle
	result := dedupeArticles(database).Where("r.id = ?", representationID).Scan(&article)
	if result.Error != nil {
		return article, result.Error
	}
	if result.RowsAffected == 0 {
		return article, gorm.ErrRecordNotFound
	}
	return article, nil
}
func dedupeSetting(database *gorm.DB) (dedupe.Setting, error) {
	var setting dedupe.Setting
	result := database.Where("id = ?", 1).Limit(1).Find(&setting)
	return setting, result.Error
}
func DedupeActive() (bool, error) {
	if db == nil {
		return false, nil
	}
	setting, err := dedupeSetting(db)
	return setting.Mode == "active" && setting.ActiveRunID != 0, err
}

// RegisterCandidateDedupe hands completed extraction to the independent queue.
// A durable pending state allows startup/periodic recovery after enqueue failure.
func RegisterCandidateDedupe(ctx context.Context, candidateID uint) error {
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidate DiscoveryCandidate
		if err := tx.First(&candidate, candidateID).Error; err != nil {
			return err
		}
		_, err := bindCandidateMaterialIdentities(tx, candidate)
		return err
	}); err != nil {
		return err
	}
	var candidate DiscoveryCandidate
	if err := db.WithContext(ctx).First(&candidate, candidateID).Error; err != nil {
		return err
	}
	if candidate.ProcessingState != DiscoveryProcessingReady {
		return nil
	}
	var article dedupeArticle
	result := dedupeArticles(db.WithContext(ctx)).Where("m.id = ?", candidate.MaterialID).Scan(&article)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	queue, ok := jobqueue.Default()
	if !ok {
		return nil
	}
	enqueuer, ok := queue.(jobqueue.DedupeEnqueuer)
	if !ok {
		return jobqueue.ErrHandlerUnavailable
	}
	return enqueuer.EnqueueDedupe(ctx, jobqueue.DedupeArgs{RepresentationID: article.RepresentationID, Algorithm: dedupe.Algorithm})
}

func indexDedupeArticle(database *gorm.DB, article dedupeArticle, signature []byte) error {
	buckets := dedupe.Buckets(signature)
	// The caller holds the generation-setting row lock. This also serializes
	// publication across concurrent runs without 16 advisory-lock round trips.

	fingerprint := dedupe.Fingerprint{RepresentationID: article.RepresentationID, Algorithm: dedupe.Algorithm, Signature: signature, NumbersHash: dedupe.NumbersHash(article.Text), Characters: dedupe.Length(article.Text), CreatedAt: time.Now()}
	insert := database.Clauses(clause.OnConflict{DoNothing: true}).Create(&fingerprint)
	if insert.Error != nil {
		return insert.Error
	}
	if insert.RowsAffected == 0 {
		return nil
	}
	rows := make([]dedupe.Bucket, 0, len(buckets))
	for band, hash := range buckets {
		rows = append(rows, dedupe.Bucket{Algorithm: dedupe.Algorithm, Band: band, Hash: hash, RepresentationID: article.RepresentationID})
	}
	return database.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// All matching representatives are streamed; a large bucket is not silently truncated.
func candidateDedupeRepresentatives(database *gorm.DB, runID uint, article dedupeArticle, signature []byte, visit func(uint) error) error {
	conditions := make([]string, 0, dedupe.Bands)
	args := []interface{}{dedupe.Algorithm, runID, dedupe.Algorithm, dedupe.NumbersHash(article.Text), float64(dedupe.Length(article.Text)) * 0.9, float64(dedupe.Length(article.Text)) / 0.9}
	for band, hash := range dedupe.Buckets(signature) {
		conditions = append(conditions, "(b.band = ? AND b.hash = ?)")
		args = append(args, band, hash)
	}
	query := `SELECT DISTINCT d.representative_id AS id FROM article_dedupe_buckets b
 JOIN article_dedupe_members d ON d.representation_id=b.representation_id
 JOIN article_dedupe_fingerprints f ON f.representation_id=b.representation_id AND f.algorithm=?
 JOIN material_representations r ON r.id=d.representation_id JOIN material m ON m.id=d.material_id AND m.current_version_id=r.version_id
 WHERE d.run_id=? AND b.algorithm=? AND f.numbers_hash=? AND f.characters>=? AND f.characters<=? AND (` + strings.Join(conditions, " OR ") + `)
 UNION SELECT DISTINCT d.representative_id AS id FROM material_representations r
 JOIN article_dedupe_members d ON d.representation_id=r.id JOIN material m ON m.id=d.material_id AND m.current_version_id=r.version_id
 WHERE d.run_id=? AND r.content_hash=?`
	args = append(args, runID, article.ContentHash)
	var cursor uint
	for {
		var rows []struct{ ID uint }
		pageArgs := append(append([]interface{}{}, args...), cursor, dedupeBatchSize)
		if err := database.Raw("SELECT id FROM ("+query+") matches WHERE id > ? ORDER BY id LIMIT ?", pageArgs...).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			if err := visit(row.ID); err != nil {
				return err
			}
			cursor = row.ID
		}
	}
}

// Assessor identity must match assessment/article_assessor.go. The per-article
// check and the report aggregate both derive it here so they cannot drift.
func dedupeAssessorName() string    { name, _ := config.ArticleAssessmentIdentity(); return name }
func dedupeAssessorVersion() string { _, model := config.ArticleAssessmentIdentity(); return model }

func hasValidDedupeAssessment(database *gorm.DB, article dedupeArticle) (bool, error) {
	if !database.Migrator().HasTable("discovery_article_assessments") {
		return false, nil
	}
	var count int64
	err := database.Table("material_article_states s").Joins("JOIN discovery_article_assessments a ON a.id=s.current_assessment_id").
		Where("s.material_id=? AND a.material_id=? AND a.content_version=? AND a.assessor=? AND a.assessor_version=? AND a.policy_version=?", article.MaterialID, article.MaterialID, article.ContentVersion, dedupeAssessorName(), dedupeAssessorVersion(), articlevalue.PolicyVersion).Count(&count).Error
	return count > 0, err
}
func usableDedupeArticle(database *gorm.DB, article dedupeArticle) (bool, error) {
	if article.CandidateID == 0 {
		return true, nil
	}
	var candidate DiscoveryCandidate
	if err := database.First(&candidate, article.CandidateID).Error; err != nil {
		return false, err
	}
	return candidate.ProcessingState == DiscoveryProcessingReady, nil
}

func selectDedupeRepresentative(database *gorm.DB, articles []dedupeArticle, current uint) (dedupeArticle, error) {
	maximum := 0
	lengths := make(map[uint]int)
	usable := make(map[uint]bool)
	for _, article := range articles {
		ok, err := usableDedupeArticle(database, article)
		if err != nil {
			return dedupeArticle{}, err
		}
		usable[article.RepresentationID] = ok
		length := dedupe.Length(article.Text)
		lengths[article.RepresentationID] = length
		if ok && length > maximum {
			maximum = length
		}
	}
	var chosen dedupeArticle
	chosenAssessed := false
	for _, article := range articles {
		if !usable[article.RepresentationID] || float64(lengths[article.RepresentationID]) < float64(maximum)*0.95 {
			continue
		}
		valid := true
		for _, other := range articles {
			if _, ok := dedupe.Verify(article.Text, other.Text); !ok {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		assessed, err := hasValidDedupeAssessment(database, article)
		if err != nil {
			return chosen, err
		}
		better := chosen.RepresentationID == 0 || (assessed && !chosenAssessed)
		if assessed == chosenAssessed && chosen.RepresentationID != 0 {
			// Retain a qualified representative. Otherwise prefer more complete text.
			better = article.RepresentationID == current || (chosen.RepresentationID != current &&
				(lengths[article.RepresentationID] > lengths[chosen.RepresentationID] || (lengths[article.RepresentationID] == lengths[chosen.RepresentationID] && article.RepresentationID < chosen.RepresentationID)))
		}
		if better {
			chosen = article
			chosenAssessed = assessed
		}
	}
	return chosen, nil
}

// Run-row locks serialize cluster revisions within one generation. Fingerprint
// computation precedes the transaction; extraction never takes these locks.
func assignDedupeMember(database *gorm.DB, runID uint, article dedupeArticle, signature []byte) error {
	started := time.Now()
	candidateCount := 0
	var run dedupe.Run
	if err := database.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, runID).Error; err != nil {
		return err
	}
	// Remove this version from its previous grouping before validating a changed representative.
	var existing dedupe.Member
	if err := database.Where("run_id=? AND representation_id=?", runID, article.RepresentationID).Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	sourceArticles := []dedupeArticle{article}
	if existing.RepresentationID != 0 && existing.RepresentativeID == article.RepresentationID {
		var sourceMembers []dedupe.Member
		if err := database.Where("run_id=? AND representative_id=?", runID, existing.RepresentativeID).Find(&sourceMembers).Error; err != nil {
			return err
		}
		for _, member := range sourceMembers {
			if member.RepresentationID == article.RepresentationID {
				continue
			}
			other, err := loadDedupeArticle(database, member.RepresentationID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			sourceArticles = append(sourceArticles, other)
		}
	}
	var bestArticles []dedupeArticle
	bestRepresentative := dedupeArticle{}
	bestScore := -1.0
	err := candidateDedupeRepresentatives(database, runID, article, signature, func(id uint) error {
		if id == article.RepresentationID {
			return nil
		}
		candidateCount++
		representative, err := loadDedupeArticle(database, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		score, ok := dedupe.Verify(article.Text, representative.Text)
		if !ok {
			return nil
		}
		usable, err := usableDedupeArticle(database, representative)
		if err != nil {
			return err
		}
		var maximum int
		if err := database.Table("article_dedupe_members d").Select("COALESCE(MAX(f.characters),0)").Joins("JOIN article_dedupe_fingerprints f ON f.representation_id=d.representation_id AND f.algorithm=?", dedupe.Algorithm).Joins("JOIN material m ON m.id=d.material_id AND m.current_version_id=d.version_id").Where("d.run_id=? AND d.representative_id=?", runID, id).Scan(&maximum).Error; err != nil {
			return err
		}
		for _, source := range sourceArticles {
			if length := dedupe.Length(source.Text); length > maximum {
				maximum = length
			}
		}
		repAssessed, err := hasValidDedupeAssessment(database, representative)
		if err != nil {
			return err
		}
		newAssessed, err := hasValidDedupeAssessment(database, article)
		if err != nil {
			return err
		}
		appendOnly := usable && float64(dedupe.Length(representative.Text)) >= float64(maximum)*0.95 && (repAssessed || !newAssessed)
		for _, source := range sourceArticles {
			if _, ok := dedupe.Verify(source.Text, representative.Text); !ok {
				appendOnly = false
				break
			}
		}
		if appendOnly {
			if score > bestScore || (score == bestScore && id < bestRepresentative.RepresentationID) {
				bestArticles = sourceArticles
				bestRepresentative = representative
				bestScore = score
			}
			return nil
		}
		var members []dedupe.Member
		if err := database.Where("run_id=? AND representative_id=?", runID, id).Order("representation_id").Find(&members).Error; err != nil {
			return err
		}
		articles := append([]dedupeArticle{}, sourceArticles...)
		seen := map[uint]bool{}
		for _, source := range sourceArticles {
			seen[source.RepresentationID] = true
		}
		for _, member := range members {
			if seen[member.RepresentationID] {
				continue
			}
			other, err := loadDedupeArticle(database, member.RepresentationID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			articles = append(articles, other)
		}
		selected, err := selectDedupeRepresentative(database, articles, id)
		if err != nil {
			return err
		}
		if selected.RepresentationID == 0 {
			return nil
		}
		if score > bestScore || (score == bestScore && selected.RepresentationID < bestRepresentative.RepresentationID) {
			bestArticles, bestRepresentative, bestScore = articles, selected, score
		}
		return nil
	})
	if err != nil {
		return err
	}
	if bestRepresentative.RepresentationID == 0 {
		selected, err := selectDedupeRepresentative(database, sourceArticles, article.RepresentationID)
		if err != nil {
			return err
		}
		if selected.RepresentationID != 0 {
			bestRepresentative = selected
			bestArticles = sourceArticles
		} else {
			bestRepresentative = article
			bestArticles = []dedupeArticle{article}
		}
	}
	// Every member is validated against the selected representative.
	for _, other := range bestArticles {
		score, _ := dedupe.Verify(other.Text, bestRepresentative.Text)
		method := "near_body"
		if other.ContentHash == bestRepresentative.ContentHash {
			method = "exact_body"
		}
		if other.RepresentationID == bestRepresentative.RepresentationID {
			method = "unique"
		}
		member := dedupe.Member{RunID: runID, RepresentationID: other.RepresentationID, MaterialID: other.MaterialID, VersionID: other.VersionID, CandidateID: other.CandidateID, RepresentativeID: bestRepresentative.RepresentationID, Similarity: score, Method: method, Revision: 1, UpdatedAt: time.Now()}
		var prior dedupe.Member
		if err := database.Where("run_id=? AND representation_id=?", runID, other.RepresentationID).Limit(1).Find(&prior).Error; err != nil {
			return err
		}
		if prior.RepresentationID != 0 {
			member.BaselinePending = prior.BaselinePending
			member.CandidateCount = prior.CandidateCount
			member.DurationMS = prior.DurationMS
			member.Revision = prior.Revision
			member.Applied = prior.Applied
			if prior.RepresentativeID != member.RepresentativeID || prior.Method != member.Method || prior.CandidateID != member.CandidateID {
				member.Revision++
				member.Applied = false
			}
		}
		if prior.RepresentationID == 0 {
			var count int64
			if err := database.Table("material_article_states s").Where("s.material_id=? AND s.assessment_state=?", other.MaterialID, "pending").Where("NOT EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=s.material_id) OR EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=s.material_id AND " + DedupeRepresentativeClause("c.") + ")").Count(&count).Error; err != nil {
				return err
			}
			member.BaselinePending = count > 0
		}
		if member.RepresentationID == article.RepresentationID {
			member.CandidateCount = candidateCount
			member.DurationMS = time.Since(started).Milliseconds()
		}
		if err := database.Clauses(clause.OnConflict{UpdateAll: true}).Create(&member).Error; err != nil {
			return err
		}
	}
	return nil
}

// DedupeQualification is computed, never a foreign article's assessment row.
type DedupeQualification struct {
	Active                   bool   `json:"active"`
	ExactOnly                bool   `json:"exactOnly"`
	Ready                    bool   `json:"ready"`
	Representative           bool   `json:"representative"`
	RepresentativeMaterialID uint   `json:"representativeMaterialId,omitempty"`
	RepresentativeVersionID  uint   `json:"representativeVersionId,omitempty"`
	Coverage                 string `json:"coverage"`
}

func ArticleDedupeQualification(database *gorm.DB, materialID uint) (DedupeQualification, error) {
	result := DedupeQualification{Ready: true, Representative: true, Coverage: "independent"}
	setting, err := dedupeSetting(database)
	if err != nil {
		return result, err
	}
	if setting.Mode == "exact" {
		result.ExactOnly = true
		return result, nil
	}
	if setting.Mode != "active" || setting.ActiveRunID == 0 {
		return result, nil
	}
	result.Active = true
	result.Ready = false
	result.Representative = false
	result.Coverage = "dedupe_pending"
	var member dedupe.Member
	lookup := database.Table("article_dedupe_members d").Select("d.*").Joins("JOIN material m ON m.id=d.material_id AND m.current_version_id=d.version_id").Where("d.run_id=? AND d.material_id=? AND d.applied=?", setting.ActiveRunID, materialID, true).Limit(1).Scan(&member)
	if lookup.Error != nil {
		return result, lookup.Error
	}
	if lookup.RowsAffected == 0 {
		return result, nil
	}
	representative, err := loadDedupeArticle(database, member.RepresentativeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	usable, err := usableDedupeArticle(database, representative)
	if err != nil {
		return result, err
	}
	if !usable {
		return result, nil
	}
	var repMember dedupe.Member
	if err := database.Where("run_id=? AND representation_id=? AND representative_id=? AND applied=?", setting.ActiveRunID, member.RepresentativeID, member.RepresentativeID, true).Limit(1).Find(&repMember).Error; err != nil {
		return result, err
	}
	if repMember.RepresentationID == 0 || repMember.CandidateID != representative.CandidateID {
		return result, nil
	}
	result.Ready = true
	result.Representative = member.RepresentationID == member.RepresentativeID
	result.RepresentativeMaterialID = representative.MaterialID
	result.RepresentativeVersionID = representative.VersionID
	if result.Representative {
		result.Coverage = "independent"
		return result, nil
	}
	valid, err := hasValidDedupeAssessment(database, representative)
	if err != nil {
		return result, err
	}
	result.Coverage = "waiting_representative"
	if valid {
		result.Coverage = "covered"
	} else {
		var state material.ArticleState
		if err := database.First(&state, "material_id=?", representative.MaterialID).Error; err != nil {
			return result, err
		}
		if state.AssessmentError != "" {
			result.Coverage = "representative_failed"
		}
	}
	return result, nil
}

// DedupeRepresentativeClause is the single projection-level gate deciding which
// candidate of a logical article may be surfaced. Assessment, recommendation,
// vector recall and operational stats must all apply it verbatim: if assessment
// were narrower than recommendation the gates could agree, but if it were wider
// the pipeline would pay for LLM assessments of articles nothing can ever show.
func DedupeRepresentativeClause(alias string) string {
	return "(" + alias + "representative_id IS NULL OR " + alias + "representative_id=" + alias + "id)"
}

// filterCurrentDedupeRepresentatives is SQL-only and keeps queue recovery bounded.
// It always narrows within DedupeRepresentativeClause, so assessment eligibility
// stays a subset of recommendation eligibility in every mode.
func FilterDedupeAssessmentCandidates(query *gorm.DB) (*gorm.DB, error) {
	setting, err := dedupeSetting(query.Session(&gorm.Session{NewDB: true}))
	if err != nil {
		return query, err
	}
	query = query.Where(DedupeRepresentativeClause("discovery_candidates."))
	if setting.Mode == "active" && setting.ActiveRunID != 0 {
		query = query.Where(`EXISTS (SELECT 1 FROM article_dedupe_members d JOIN material m ON m.id=d.material_id AND CASE WHEN m.current_version_id=d.version_id THEN TRUE ELSE FALSE END
  WHERE d.run_id=? AND d.material_id=discovery_candidates.material_id AND (d.representation_id=d.representative_id) IS TRUE AND d.applied=?)`, setting.ActiveRunID, true)
	} else if setting.Mode == "exact" {
		// Rollback disables near-duplicate suppression without undoing strong identities.
		query = query.Where("discovery_candidates.id=(SELECT MIN(c.id) FROM discovery_candidates c WHERE c.material_id=discovery_candidates.material_id AND c.processing_state='ready')")
	}
	return query, nil
}

func markDedupeFailure(ctx context.Context, runID uint, err error) error {
	if runID != 0 {
		_ = db.WithContext(ctx).Model(&dedupe.Run{}).Where("id=?", runID).Update("error", err.Error()).Error
	}
	return err
}
func enqueueDedupeTask(ctx context.Context, args jobqueue.DedupeArgs) error {
	queue, ok := jobqueue.Default()
	if !ok {
		return jobqueue.ErrHandlerUnavailable
	}
	enqueuer, ok := queue.(jobqueue.DedupeEnqueuer)
	if !ok {
		return jobqueue.ErrHandlerUnavailable
	}
	return enqueuer.EnqueueDedupe(ctx, args)
}

func jsonProjection(candidate DiscoveryCandidate) string {
	values := map[string]interface{}{"duplicate_cluster_id": candidate.DuplicateClusterID, "representative_id": candidate.RepresentativeID, "dedupe_key": candidate.DedupeKey, "dedupe_state": candidate.DedupeState, "eligibility_state": candidate.EligibilityState, "eligibility_reasons": candidate.EligibilityReasons}
	body, _ := json.Marshal(values)
	return string(body)
}

// LockDedupeMaterialState takes the per-material row lock that orders dedupe
// projection writes against assessment activation. Both paths must acquire it
// before reading or writing that material's membership or article state. One
// lock type acquired in that order is deadlock-free, and unlike a global
// setting-row lock it never serializes unrelated materials behind a dedupe batch.
func LockDedupeMaterialState(database *gorm.DB, materialID uint) error {
	if materialID == 0 {
		return nil
	}
	var state material.ArticleState
	return database.Clauses(clause.Locking{Strength: "UPDATE"}).Where("material_id = ?", materialID).Limit(1).Find(&state).Error
}

// DedupeAssessmentAllowed reports whether an assessment result may be activated.
// The caller must already hold LockDedupeMaterialState for materialID and keep
// it until commit; that lock, not the dedupe setting row, provides the ordering.
func DedupeAssessmentAllowed(database *gorm.DB, materialID uint) (bool, error) {
	qualification, err := ArticleDedupeQualification(database, materialID)
	return !qualification.Active || (qualification.Ready && qualification.Representative), err
}
