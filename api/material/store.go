package material

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func Hash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

func TextHash(text string) string { return Hash(strings.Join(strings.Fields(text), " ")) }

func Authors(author string) string {
	values := []string{}
	if strings.TrimSpace(author) != "" {
		values = append(values, strings.TrimSpace(author))
	}
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func FirstAuthor(authors string) string {
	var values []string
	if json.Unmarshal([]byte(authors), &values) == nil && len(values) > 0 {
		return values[0]
	}
	return ""
}

func ResolveID(tx *gorm.DB, id uint) (uint, error) {
	seen := map[uint]bool{}
	for id != 0 {
		if seen[id] {
			return 0, fmt.Errorf("material redirect cycle at %d", id)
		}
		seen[id] = true
		var redirect Redirect
		err := tx.First(&redirect, id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return id, nil
		}
		if err != nil {
			return 0, err
		}
		id = redirect.MaterialID
	}
	return 0, gorm.ErrRecordNotFound
}

// EnsureURL serializes identity creation using an advisory lock on PostgreSQL.
// The caller owns the transaction, including the discovery/archival write.
func EnsureURL(tx *gorm.DB, url, title, summary string) (Material, error) {
	var record Material
	if normalized, err := NormalizeURL(url); err == nil {
		url = normalized
	}
	key := Hash(url)
	if tx.Dialector.Name() == "postgres" {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "material:url:"+key).Error; err != nil {
			return record, err
		}
	}
	var identity Identity
	err := tx.Where("kind = ? AND identity_key = ?", "url", key).First(&identity).Error
	if err == nil {
		id, resolveErr := ResolveID(tx, identity.MaterialID)
		if resolveErr != nil {
			return record, resolveErr
		}
		err = tx.First(&record, id).Error
		return record, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return record, err
	}
	record = Material{Title: title, Summary: summary, Authors: "[]", Topics: "[]", Entities: "[]"}
	if err := tx.Create(&record).Error; err != nil {
		return record, err
	}
	identity = Identity{MaterialID: record.ID, Kind: "url", IdentityKey: key, Value: url}
	if err := tx.Create(&identity).Error; err != nil {
		return record, err
	}
	return record, tx.Create(&ArticleState{MaterialID: record.ID}).Error
}

func SourceCounts(tx *gorm.DB, ids []uint) (map[uint]int, error) {
	result := make(map[uint]int)
	if len(ids) == 0 {
		return result, nil
	}
	var rows []struct {
		MaterialID uint
		Count      int
	}
	err := tx.Model(&Provenance{}).Select("material_id, COUNT(DISTINCT domain_key) AS count").
		Where("material_id IN ? AND domain_key <> ''", ids).Group("material_id").Scan(&rows).Error
	for _, row := range rows {
		result[row.MaterialID] = row.Count
	}
	return result, err
}

// AppendText creates an immutable version only when the extracted text changes.
// It never treats a physical file checksum as a content identity.
func AppendText(tx *gorm.DB, record Material, body string, words int, extractor string, at time.Time) (Version, error) {
	var version Version
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, record.ID).Error; err != nil {
		return version, err
	}
	if record.CurrentVersionID != nil {
		var current Representation
		if err := tx.Where("version_id = ? AND kind = ? AND role = ?", *record.CurrentVersionID, "text", "body").First(&current).Error; err == nil && current.ContentHash == TextHash(body) {
			err = tx.First(&version, *record.CurrentVersionID).Error
			return version, err
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return version, err
		}
	}
	var maximum uint
	if err := tx.Model(&Version{}).Where("material_id = ?", record.ID).Select("COALESCE(MAX(version), 0)").Scan(&maximum).Error; err != nil {
		return version, err
	}
	version = Version{MaterialID: record.ID, Version: maximum + 1, Title: record.Title, Summary: record.Summary, Authors: record.Authors, Language: record.Language, PublishedAt: record.PublishedAt, CreatedAt: at}
	if err := tx.Create(&version).Error; err != nil {
		return version, err
	}
	representation := Representation{VersionID: version.ID, Kind: "text", Role: "body", Text: body, ContentHash: TextHash(body), WordCount: words, Extractor: extractor, CreatedAt: at}
	if err := tx.Create(&representation).Error; err != nil {
		return version, err
	}
	return version, tx.Model(&Material{}).Where("id = ?", record.ID).Update("current_version_id", version.ID).Error
}
