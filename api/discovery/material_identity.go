package discovery

import (
	"DataArk/material"
	"gorm.io/gorm"
	"strings"
	"time"
)

func bindCandidateMaterialIdentities(tx *gorm.DB, candidate DiscoveryCandidate) (uint, error) {
	id := candidate.MaterialID
	values := []string{candidate.URL, candidate.NormalizedURL}
	if candidate.FetchedAt != nil || candidate.ProcessingState == DiscoveryProcessingReady {
		values = append(values, candidate.FinalURL)
		finalDomain, _ := domainKeyForURL(candidate.FinalURL)
		canonicalDomain, _ := domainKeyForURL(candidate.CanonicalURL)
		if finalDomain != "" && finalDomain == canonicalDomain {
			values = append(values, candidate.CanonicalURL)
		}
	}
	for _, value := range uniqueSortedStrings(values) {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if normalized, err := NormalizeArticleURL(value); err == nil {
			value = normalized
		}
		var err error
		id, err = material.BindIdentity(tx, id, "url", value)
		if err != nil {
			return 0, err
		}
	}
	if len([]rune(strings.TrimSpace(candidate.BodyText))) >= 120 && candidate.ProcessingState == DiscoveryProcessingReady {
		var err error
		id, err = material.BindIdentity(tx, id, "text:sha256-text-v1", material.TextHash(candidate.BodyText))
		if err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ReconcileMaterialIdentities is a restartable data migration. DDL remains in
// Goose. Existing near-duplicate clusters are not evidence for identity merges.
func ReconcileMaterialIdentities(database *gorm.DB) error {
	if database == nil || database.Dialector.Name() != "postgres" {
		return nil
	}
	var completed int64
	if err := database.Table("material_migration_checkpoints").Where("name = ?", "strong-identities").Count(&completed).Error; err != nil {
		return err
	}
	if completed != 0 {
		return nil
	}
	var lastID uint
	for {
		var candidates []DiscoveryCandidate
		if err := Candidates(database).Where("id > ?", lastID).Order("id").Limit(250).Find(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			break
		}
		for _, candidate := range candidates {
			if err := database.Transaction(func(tx *gorm.DB) error { _, err := bindCandidateMaterialIdentities(tx, candidate); return err }); err != nil {
				return err
			}
			lastID = candidate.ID
		}
	}
	return database.Exec("INSERT INTO material_migration_checkpoints(name, completed_at) VALUES (?, ?)", "strong-identities", time.Now()).Error
}
