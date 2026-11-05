package material

import (
	"strings"

	"gorm.io/gorm"
)

// ArticleReadySQL tests retained content, never the state of a fetching job.
// materialID is a trusted SQL column expression supplied by the application.
func ArticleReadySQL(materialID string) string {
	return `EXISTS (SELECT 1 FROM material content_m
 JOIN material_versions content_v ON content_v.id=content_m.current_version_id AND content_v.material_id=content_m.id
 JOIN material_article_states content_s ON content_s.material_id=content_m.id AND content_s.content_version=content_v.version
 JOIN material_representations content_r ON content_r.version_id=content_v.id AND content_r.kind='text' AND content_r.role='body'
 WHERE content_m.id=` + materialID + ` AND content_r.text<>'')`
}

func ArticleReady(database *gorm.DB, id uint) (bool, error) {
	var count int64
	err := database.Model(&Material{}).Where("id = ?", id).Where(ArticleReadySQL("material.id")).Count(&count).Error
	return count > 0, err
}

// ArticleBodyReady is used for a current content projection already loaded.
func ArticleBodyReady(materialID, version uint, body string) bool {
	return materialID != 0 && version != 0 && strings.TrimSpace(body) != ""
}
