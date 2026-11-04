package search

import (
	"DataArk/archive"
	"DataArk/material"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// Document is the complete, replaceable search projection of a material.
type Document struct {
	ID          string            `json:"id"`
	MaterialID  uint              `json:"materialId"`
	Title       string            `json:"title"`
	Summary     string            `json:"summary"`
	Keywords    []string          `json:"keywords"`
	Content     string            `json:"content"`
	Link        string            `json:"link"`
	Domain      string            `json:"domain"`
	Filename    string            `json:"filename"`
	Archived    bool              `json:"archived"`
	ArchivePath string            `json:"archivePath"`
	Archives    []ArchiveLocation `json:"archives"`
}

type ArchiveLocation struct {
	Domain   string `json:"domain"`
	Filename string `json:"filename"`
}

func documentID(id uint) string { return "material_" + strconv.FormatUint(uint64(id), 10) }

func existingArchive(domain, filename string) string {
	// Escape each segment so filenames containing #, ?, %, spaces work as URLs.
	parts := strings.Split(filepath.ToSlash(filename), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	path := "/archive/" + url.PathEscape(domain) + "/" + strings.Join(parts, "/")
	resolved, err := archive.ResolveArchiveDocumentPath(path)
	if err != nil {
		return ""
	}
	info, err := os.Stat(resolved.AbsPath)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return path
}

func loadArchives(db *gorm.DB, ids []uint) (map[uint][]archive.ArchiveDocument, error) {
	rows := []archive.ArchiveDocument{}
	if len(ids) > 0 {
		if err := db.Where("material_id IN ?", ids).Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
			return nil, err
		}
	}
	result := map[uint][]archive.ArchiveDocument{}
	for _, row := range rows {
		result[row.MaterialID] = append(result[row.MaterialID], row)
	}
	return result, nil
}

func buildMaterialDocument(db *gorm.DB, id uint) (*Document, error) {
	var core material.Material
	err := db.First(&core, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	doc := &Document{ID: documentID(id), MaterialID: id, Title: core.Title, Summary: core.Summary, Keywords: []string{}, Archives: []ArchiveLocation{}}
	var state material.ArticleState
	if err := db.Where("material_id = ?", id).Limit(1).Find(&state).Error; err != nil {
		return nil, err
	}
	// A historical assessment does not qualify a new body version.
	var active struct {
		ID       uint
		Summary  string
		Keywords string
	}
	if state.AssessmentState == "ready" && state.CurrentAssessmentID != nil && core.CurrentVersionID != nil {
		err := db.Table("discovery_article_assessments").Select("id, summary, keywords").
			Where("id = ? AND material_id = ? AND content_version = ? AND assessor <> ? AND assessor <> ''", *state.CurrentAssessmentID, id, state.ContentVersion, "deterministic_rules").Limit(1).Find(&active).Error
		if err != nil {
			return nil, err
		}
	}
	if active.ID != 0 {
		var body material.Representation
		if err := db.Where("version_id = ? AND kind = 'text' AND role = 'body'", *core.CurrentVersionID).Limit(1).Find(&body).Error; err != nil {
			return nil, err
		}
		// The version itself must match the assessment projection.
		var version material.Version
		if err := db.First(&version, *core.CurrentVersionID).Error; err != nil {
			return nil, err
		}
		if version.Version == state.ContentVersion {
			doc.Content = body.Text
			doc.Summary = active.Summary
			decodeKeywords(active.Keywords, &doc.Keywords)
		}
	}
	var identities []material.Identity
	if err := db.Where("material_id = ? AND kind = 'url'", id).Order("id").Find(&identities).Error; err != nil {
		return nil, err
	}
	for _, identity := range identities {
		if validSourceURL(identity.Value) {
			doc.Link = identity.Value
			break
		}
	}
	archives, err := loadArchives(db, []uint{id})
	if err != nil {
		return nil, err
	}
	for _, row := range archives[id] {
		path := existingArchive(row.Domain, row.FileName)
		if path == "" {
			continue
		}
		doc.Archives = append(doc.Archives, ArchiveLocation{Domain: row.Domain, Filename: row.FileName})
		if doc.Archived {
			continue
		}
		doc.Archived, doc.ArchivePath, doc.Domain, doc.Filename = true, path, row.Domain, row.FileName
		if validSourceURL(row.SourceURL) {
			doc.Link = row.SourceURL
		}
		if strings.TrimSpace(doc.Content) == "" {
			resolved, _ := archive.ResolveArchiveDocumentPath(path)
			html, err := archive.GetHTMLFileContent(resolved.AbsPath)
			if err != nil {
				return nil, err
			}
			text, err := archive.ExtractHTMLText(html)
			if err != nil {
				return nil, err
			}
			doc.Content, doc.Title, doc.Summary = text, row.Title, row.Summary
		}
	}
	if !doc.Archived && (strings.TrimSpace(doc.Content) == "" || !validSourceURL(doc.Link)) {
		return nil, nil
	}
	if doc.Title == "" {
		doc.Title = doc.Link
		if doc.Title == "" {
			doc.Title = doc.Filename
		}
	}
	if doc.Domain == "" {
		parsed, _ := url.Parse(doc.Link)
		if parsed != nil {
			doc.Domain = parsed.Hostname()
		}
	}
	return doc, nil
}

func validSourceURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}
