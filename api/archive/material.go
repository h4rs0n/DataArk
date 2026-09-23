package archive

import (
	"DataArk/config"
	"DataArk/material"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"os"
	"strings"
	"time"
)

// EnsureContentMaterial associates searchable content without putting physical
// archive names or file metadata in the material core.
func EnsureContentMaterial(domain, filename, sourceURL, title, body string) (uint, error) {
	if db == nil {
		return 0, nil
	}
	var id uint
	err := db.Transaction(func(tx *gorm.DB) error {
		identity := strings.TrimSpace(sourceURL)
		if identity == "" {
			identity = "archive:" + domain + "/" + filename
		}
		record, err := material.EnsureURL(tx, identity, title, BuildSummary(body, 220))
		if err != nil {
			return err
		}
		id = record.ID
		if record.CurrentVersionID == nil && strings.TrimSpace(body) != "" {
			version, err := material.AppendText(tx, record, body, len(strings.Fields(body)), "archive_html", time.Now())
			if err != nil {
				return err
			}
			if err := tx.Model(&material.ArticleState{}).Where("material_id = ?", id).Update("content_version", version.Version).Error; err != nil {
				return err
			}
			if len([]rune(body)) >= 120 {
				id, err = material.BindIdentity(tx, id, "text:sha256-text-v1", material.TextHash(body))
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	return id, err
}

// BackfillMaterialContent runs locally during maintenance startup. Damaged or
// absent files remain represented by metadata and a persistent repair issue.
func BackfillMaterialContent() error {
	if db == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	var done int64
	if err := db.Table("material_migration_checkpoints").Where("name = ?", "archive-content").Count(&done).Error; err != nil {
		return err
	}
	if done != 0 {
		return nil
	}
	var documents []ArchiveDocument
	if err := db.Order("id").Find(&documents).Error; err != nil {
		return err
	}
	for _, document := range documents {
		identity := document.SourceURL
		if identity == "" {
			identity = "archive:" + document.Domain + "/" + document.FileName
		}
		if normalized, err := material.NormalizeURL(identity); err == nil {
			identity = normalized
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			_, err := material.BindIdentity(tx, document.MaterialID, "url", identity)
			return err
		}); err != nil {
			return err
		}
		var fileErr error
		var body []byte
		if config.ARCHIVEFILELOACTION == "" {
			fileErr = fmt.Errorf("archive directory is not configured")
		} else {
			resolved, err := ResolveArchiveDocumentPath("/archive/" + document.Domain + "/" + document.FileName)
			if err != nil {
				fileErr = err
			} else {
				body, fileErr = os.ReadFile(resolved.AbsPath)
			}
		}
		text := ""
		if fileErr == nil {
			text, fileErr = ExtractHTMLText(string(body))
		}
		if fileErr != nil {
			issue := map[string]interface{}{"archive_document_id": document.ID, "error": fileErr.Error(), "updated_at": time.Now()}
			if err := db.Table("material_ingestion_issues").Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "archive_document_id"}}, DoUpdates: clause.AssignmentColumns([]string{"error", "updated_at"})}).Create(issue).Error; err != nil {
				return err
			}
			continue
		}
		id, err := EnsureContentMaterial(document.Domain, document.FileName, document.SourceURL, document.Title, text)
		if err != nil {
			return err
		}
		if err := db.Model(&ArchiveDocument{}).Where("id = ?", document.ID).Update("material_id", id).Error; err != nil {
			return err
		}
		if err := db.Exec("DELETE FROM material_ingestion_issues WHERE archive_document_id = ?", document.ID).Error; err != nil {
			return err
		}
	}
	return db.Exec("INSERT INTO material_migration_checkpoints(name) VALUES (?)", "archive-content").Error
}
