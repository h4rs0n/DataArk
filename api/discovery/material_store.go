package discovery

import (
	"DataArk/material"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Candidate content fields are a transport projection, not candidate columns.
var materialColumns = []string{"title", "summary", "language", "published_at", "topics", "entities"}
var articleColumns = []string{"content_version", "body_changed_at", "content_type", "content_style", "quality_score", "depth_score", "assessment_state", "current_assessment_id", "assessment_error", "eligibility_state", "eligibility_reasons", "enrichment_status", "enrichment_error", "llm_model", "prompt_version", "enriched_at", "score"}
var provenanceColumns = []string{"source_id", "source_name", "metadata_confidence", "published_confidence"}

// Candidates is the read projection for existing HTTP/selection consumers.
// Writes must use UpdateCandidate(s), never update this derived table.
func Candidates(database *gorm.DB) *gorm.DB {
	return database.Table("discovery_candidate_details AS discovery_candidates")
}

func (candidate *DiscoveryCandidate) BeforeCreate(tx *gorm.DB) error {
	if candidate.MaterialID == 0 {
		url := candidate.NormalizedURL
		if url == "" {
			url = candidate.URL
		}
		record, err := material.EnsureURL(tx, url, candidate.Title, candidate.Summary)
		if err != nil {
			return err
		}
		candidate.MaterialID = record.ID
	}
	return nil
}

func (candidate *DiscoveryCandidate) AfterCreate(tx *gorm.DB) error {
	values := candidateContentValues(candidate)
	var others int64
	if err := tx.Model(&DiscoveryCandidate{}).Where("material_id = ? AND id <> ?", candidate.MaterialID, candidate.ID).Count(&others).Error; err != nil {
		return err
	}
	var core material.Material
	if err := tx.First(&core, candidate.MaterialID).Error; err != nil {
		return err
	}
	// Another discovery of an existing work must not reset its assessment or
	// replace extracted content with empty feed metadata.
	if others == 0 && (core.CurrentVersionID == nil || candidate.BodyText != "") {
		if err := updateCandidateContent(tx, candidate, values); err != nil {
			return err
		}
	}
	if candidate.SourceID != 0 && !candidate.SkipInitialProvenance {
		if err := saveCandidateSource(tx, candidate, candidate.SourceID, candidate.SourceName); err != nil {
			return err
		}
	}
	return Candidates(tx.Session(&gorm.Session{NewDB: true})).Where("discovery_candidates.id = ?", candidate.ID).Scan(candidate).Error
}

func (candidate *DiscoveryCandidate) BeforeUpdate(tx *gorm.DB) error {
	if value, ok := tx.Statement.Dest.(*DiscoveryCandidate); ok && value.ID != 0 {
		return updateCandidateContent(tx.Session(&gorm.Session{NewDB: true}), value, candidateContentValues(value))
	}
	return nil
}

func (candidate *DiscoveryCandidate) AfterFind(tx *gorm.DB) error {
	if candidate.MaterialID == 0 || tx.Statement.Table == "discovery_candidate_details" {
		return nil
	}
	if tx.Statement.TableExpr != nil && strings.Contains(tx.Statement.TableExpr.SQL, "discovery_candidate_details") {
		return nil
	}
	return Candidates(tx.Session(&gorm.Session{NewDB: true})).Where("discovery_candidates.id = ?", candidate.ID).Scan(candidate).Error
}

func candidateContentValues(candidate *DiscoveryCandidate) map[string]interface{} {
	values := map[string]interface{}{}
	value := reflect.ValueOf(candidate).Elem()
	for _, field := range txCandidateFields() {
		values[field.column] = value.FieldByName(field.name).Interface()
	}
	return values
}

type candidateField struct{ name, column string }

func txCandidateFields() []candidateField {
	return []candidateField{
		{"Title", "title"}, {"Summary", "summary"}, {"Author", "author"}, {"Language", "language"}, {"PublishedAt", "published_at"}, {"Topics", "topics"}, {"Entities", "entities"},
		{"BodyText", "body_text"}, {"WordCount", "word_count"}, {"ContentHash", "content_hash"}, {"ContentVersion", "content_version"}, {"BodyChangedAt", "body_changed_at"},
		{"ContentType", "content_type"}, {"ContentStyle", "content_style"}, {"QualityScore", "quality_score"}, {"DepthScore", "depth_score"}, {"AssessmentState", "assessment_state"}, {"CurrentAssessmentID", "current_assessment_id"}, {"AssessmentError", "assessment_error"}, {"EligibilityState", "eligibility_state"}, {"EligibilityReasons", "eligibility_reasons"},
		{"EnrichmentStatus", "enrichment_status"}, {"EnrichmentError", "enrichment_error"}, {"LLMModel", "llm_model"}, {"PromptVersion", "prompt_version"}, {"EnrichedAt", "enriched_at"}, {"Score", "score"}, {"ArchivedTaskID", "archived_task_id"},
	}
}

func saveCandidateSource(tx *gorm.DB, candidate *DiscoveryCandidate, sourceID uint, name string) error {
	var source DiscoverySource
	err := tx.First(&source, sourceID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var domain string
	if source.SiteID != nil {
		var site DiscoverySite
		if err := tx.First(&site, *source.SiteID).Error; err != nil {
			return err
		}
		domain = siteDomainKey(site)
	} else if source.URL != "" {
		domain, _ = domainKeyForURL(source.URL)
	}
	now := candidate.CreatedAt
	if now.IsZero() {
		now = time.Now()
	}
	row := material.Provenance{ProvenanceKey: fmt.Sprintf("candidate:%d:source:%d", candidate.ID, sourceID), MaterialID: candidate.MaterialID,
		CandidateID: &candidate.ID, SiteID: source.SiteID, SourceID: &sourceID, DomainKey: domain, SourceName: name,
		DiscoveryMethod: "legacy_source", OriginalURL: candidate.URL, Title: candidate.Title, Summary: candidate.Summary,
		MetadataConfidence: candidate.MetadataConfidence, PublishedConfidence: candidate.PublishedConfidence, PublishedAt: candidate.PublishedAt, FirstSeenAt: now, LastSeenAt: candidate.LastSeenAt}
	// Test fixtures may carry a historical source ID without an extant source.
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row.SourceID = nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

func UpdateCandidate(database *gorm.DB, id uint, values map[string]interface{}) *gorm.DB {
	return UpdateCandidates(database.Model(&DiscoveryCandidate{}).Where("id = ?", id), values)
}

// UpdateCandidates keeps a content mutation and its crawl-state mutation atomic.
func UpdateCandidates(query *gorm.DB, values map[string]interface{}) *gorm.DB {
	// Pluck changes the destination to []uint; GORM no longer infers the
	// primary key from Model(&candidate), so retain that scope explicitly.
	if candidate, ok := query.Statement.Model.(*DiscoveryCandidate); ok && candidate.ID != 0 {
		query = query.Where("id = ?", candidate.ID)
	}
	result := query.Session(&gorm.Session{NewDB: true})
	var ids []uint
	if err := query.Pluck("id", &ids).Error; err != nil {
		result.AddError(err)
		return result
	}
	err := result.Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			var candidate DiscoveryCandidate
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&candidate, id).Error; err != nil {
				return err
			}
			if err := updateCandidateContent(tx, &candidate, values); err != nil {
				return err
			}
			crawl := map[string]interface{}{}
			for key, value := range values {
				if !isContentColumn(key) {
					crawl[key] = value
				}
			}
			if len(crawl) > 0 {
				if err := tx.Model(&DiscoveryCandidate{}).Where("id = ?", id).Updates(crawl).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	result.AddError(err)
	if err == nil {
		result.RowsAffected = int64(len(ids))
	}
	return result
}

func isContentColumn(key string) bool {
	for _, group := range [][]string{materialColumns, articleColumns, provenanceColumns, {"author", "body_text", "word_count", "content_hash", "archived_task_id", "embedding_model"}} {
		for _, column := range group {
			if key == column {
				return true
			}
		}
	}
	return false
}

func updateCandidateContent(tx *gorm.DB, candidate *DiscoveryCandidate, values map[string]interface{}) error {
	var locked material.Material
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, candidate.MaterialID).Error; err != nil {
		return err
	}
	core, state := map[string]interface{}{}, map[string]interface{}{}
	provenance := map[string]interface{}{}
	for _, column := range provenanceColumns {
		if value, ok := values[column]; ok {
			provenance[column] = value
		}
	}
	if len(provenance) > 0 {
		if err := tx.Model(&material.Provenance{}).Where("candidate_id = ?", candidate.ID).Updates(provenance).Error; err != nil {
			return err
		}
	}
	for _, column := range materialColumns {
		if value, ok := values[column]; ok {
			if (column == "topics" || column == "entities") && strings.TrimSpace(fmt.Sprint(value)) == "" {
				value = "[]"
			}
			core[column] = value
		}
	}
	if value, ok := values["author"]; ok {
		core["authors"] = material.Authors(fmt.Sprint(value))
	}
	if len(core) > 0 {
		if err := tx.Model(&material.Material{}).Where("id = ?", candidate.MaterialID).Updates(core).Error; err != nil {
			return err
		}
	}
	for _, column := range articleColumns {
		if value, ok := values[column]; ok {
			state[column] = value
		}
	}
	if state["assessment_state"] == "" {
		state["assessment_state"] = "pending"
	}
	if state["eligibility_state"] == "" {
		state["eligibility_state"] = "unknown"
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&material.ArticleState{MaterialID: candidate.MaterialID}).Error; err != nil {
		return err
	}
	if body, ok := values["body_text"].(string); ok && body != "" {
		var record material.Material
		if err := tx.First(&record, candidate.MaterialID).Error; err != nil {
			return err
		}
		words := candidate.WordCount
		if value, ok := values["word_count"].(int); ok {
			words = value
		}
		version, err := material.AppendText(tx, record, body, words, "article", time.Now())
		if err != nil {
			return err
		}
		state["content_version"] = version.Version
		legacyVersion := candidate.ContentVersion
		if value, ok := values["content_version"].(uint); ok {
			legacyVersion = value
		}
		if legacyVersion == 0 {
			legacyVersion = version.Version
		}
		mapping := material.CandidateVersion{CandidateID: candidate.ID, ContentVersion: legacyVersion, MaterialID: candidate.MaterialID, VersionID: version.ID}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&mapping).Error; err != nil {
			return err
		}
	}
	if len(state) > 0 {
		if err := tx.Model(&material.ArticleState{}).Where("material_id = ?", candidate.MaterialID).Updates(state).Error; err != nil {
			return err
		}
	}
	if task, ok := values["archived_task_id"].(string); ok && task != "" {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&material.ArchiveLink{TaskID: task, MaterialID: candidate.MaterialID}).Error; err != nil {
			return err
		}
	}
	return nil
}
