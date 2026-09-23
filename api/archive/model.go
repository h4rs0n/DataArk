package archive

import (
	"DataArk/material"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var db *gorm.DB

// ArchiveTask HTML 离线归档任务。
type ArchiveTask struct {
	ID             string     `json:"id" gorm:"primaryKey;size:36"`
	URL            string     `json:"url" gorm:"index;not null"`
	Domain         string     `json:"domain" gorm:"not null"`
	Status         string     `json:"status" gorm:"index;not null"`
	FileName       string     `json:"fileName"`
	Error          string     `json:"error" gorm:"type:text"`
	ExternalTaskID string     `json:"externalTaskId"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	StartedAt      *time.Time `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
}

// ArchiveStat HTML 归档统计。
type ArchiveStat struct {
	Source    string    `json:"source" gorm:"primaryKey;size:255"`
	FileCount int       `json:"fileCount" gorm:"not null;default:0"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ArchiveDocument 保存单个归档 HTML 的可搜索元数据。
type ArchiveDocument struct {
	MaterialID uint      `json:"materialId" gorm:"index"`
	ID         uint      `json:"id" gorm:"primaryKey"`
	Domain     string    `json:"domain" gorm:"uniqueIndex:idx_archive_documents_identity;not null;size:255"`
	FileName   string    `json:"fileName" gorm:"uniqueIndex:idx_archive_documents_identity;not null;size:1024"`
	SourceURL  string    `json:"sourceUrl"`
	Title      string    `json:"title" gorm:"size:1024"`
	Summary    string    `json:"summary" gorm:"type:text"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type SearchEvent struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Keyword     string    `json:"keyword" gorm:"index;not null;size:255"`
	ResultCount int       `json:"resultCount" gorm:"not null;default:0"`
	CreatedAt   time.Time `json:"createdAt" gorm:"index"`
}

type ArchiveClickEvent struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Domain    string    `json:"domain" gorm:"index;not null;size:255"`
	FileName  string    `json:"fileName" gorm:"index;not null;size:1024"`
	Path      string    `json:"path" gorm:"index;not null;size:1400"`
	Keyword   string    `json:"keyword" gorm:"size:255"`
	CreatedAt time.Time `json:"createdAt" gorm:"index"`
}

// ArchiveStatsSnapshot 是接口返回的统计快照，总数由各来源数量求和得到。
type ArchiveStatsSnapshot struct {
	TotalFiles int               `json:"totalFiles"`
	Sources    []ArchiveStatItem `json:"sources"`
}

// ArchiveStatItem 表示单个 URL 来源的 HTML 文件数量。
type ArchiveStatItem struct {
	Source    string `json:"source"`
	FileCount int    `json:"fileCount"`
}

func SetDB(database *gorm.DB) *gorm.DB {
	oldDB := db
	db = database
	return oldDB
}

func CreateArchiveTask(task *ArchiveTask) error {
	return db.Create(task).Error
}

func SaveArchiveTask(task *ArchiveTask) error {
	return db.Save(task).Error
}

func GetArchiveTaskByID(id string) (*ArchiveTask, error) {
	var task ArchiveTask
	if err := db.First(&task, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func GetLatestArchiveTaskByURL(rawURL string) (*ArchiveTask, error) {
	var task ArchiveTask
	if err := db.Where("url = ?", rawURL).Order("created_at desc").First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func FindActiveArchiveTaskByURL(rawURL string) (*ArchiveTask, error) {
	var task ArchiveTask
	if err := db.Where("url = ? AND status IN ?", rawURL, []string{"pending", "running"}).
		Order("created_at desc").
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func ListArchiveTasksByStatuses(statuses []string) ([]ArchiveTask, error) {
	var tasks []ArchiveTask
	if err := db.Where("status IN ?", statuses).Order("created_at asc").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func SaveArchiveDocumentMetadata(domain string, fileName string, sourceURL string) error {
	return SaveArchiveDocumentDetails(domain, fileName, sourceURL, "", "")
}

func SaveArchiveDocumentDetails(domain string, fileName string, sourceURL string, title string, summary string) error {
	domain = strings.TrimSpace(domain)
	fileName = strings.TrimSpace(fileName)
	sourceURL = strings.TrimSpace(sourceURL)
	if db == nil || domain == "" || fileName == "" {
		return nil
	}

	document := ArchiveDocument{
		Domain:    domain,
		FileName:  fileName,
		SourceURL: sourceURL,
		Title:     strings.TrimSpace(title),
		Summary:   strings.TrimSpace(summary),
	}
	updates := map[string]interface{}{
		"source_url":  sourceURL,
		"material_id": gorm.Expr("excluded.material_id"),
		"updated_at":  time.Now(),
	}
	if strings.TrimSpace(title) != "" {
		updates["title"] = strings.TrimSpace(title)
	}
	if strings.TrimSpace(summary) != "" {
		updates["summary"] = strings.TrimSpace(summary)
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "domain"}, {Name: "file_name"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&document).Error
}

func GetArchiveDocument(domain string, fileName string) (*ArchiveDocument, error) {
	domain = strings.TrimSpace(domain)
	fileName = strings.TrimSpace(fileName)
	if db == nil || domain == "" || fileName == "" {
		return nil, gorm.ErrRecordNotFound
	}

	var document ArchiveDocument
	result := db.Where("domain = ? AND file_name = ?", domain, fileName).Limit(1).Find(&document)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &document, nil
}

func GetArchiveDocumentSourceURL(domain string, fileName string) (string, error) {
	domain = strings.TrimSpace(domain)
	fileName = strings.TrimSpace(fileName)
	if db == nil || domain == "" || fileName == "" {
		return "", nil
	}

	var document ArchiveDocument
	result := db.Where("domain = ? AND file_name = ?", domain, fileName).Limit(1).Find(&document)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", nil
	}
	return document.SourceURL, nil
}

func DeleteArchiveDocumentMetadata(domain string, fileName string) error {
	domain = strings.TrimSpace(domain)
	fileName = strings.TrimSpace(fileName)
	if db == nil || domain == "" || fileName == "" {
		return nil
	}
	return db.Where("domain = ? AND file_name = ?", domain, fileName).Delete(&ArchiveDocument{}).Error
}

// GetArchiveStats 读取当前统计快照，并在内存中汇总 HTML 文件总数。
func GetArchiveStats() (*ArchiveStatsSnapshot, error) {
	var stats []ArchiveStat
	if err := db.Order("source asc").Find(&stats).Error; err != nil {
		return nil, err
	}
	return buildArchiveStatsSnapshot(stats), nil
}

// ReplaceArchiveStats 用一份完整快照替换数据库里的归档统计。
func ReplaceArchiveStats(stats []ArchiveStat) (*ArchiveStatsSnapshot, error) {
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ArchiveStat{}).Error; err != nil {
			return err
		}
		if len(stats) == 0 {
			return nil
		}
		return tx.Create(&stats).Error
	}); err != nil {
		return nil, err
	}
	return buildArchiveStatsSnapshot(stats), nil
}

// IncrementArchiveStat 在新增归档文件后增量更新对应来源的统计数量。
func IncrementArchiveStat(source string, delta int) error {
	source = strings.TrimSpace(source)
	if source == "" || delta == 0 {
		return nil
	}
	stat := ArchiveStat{
		Source:    source,
		FileCount: delta,
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "source"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"file_count": gorm.Expr("archive_stats.file_count + ?", delta),
			"updated_at": time.Now(),
		}),
	}).Create(&stat).Error
}

// DecrementArchiveStat 在删除归档文件后更新缓存统计。
func DecrementArchiveStat(source string, delta int) error {
	source = strings.TrimSpace(source)
	if source == "" || delta <= 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var stat ArchiveStat
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stat, "source = ?", source).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		stat.FileCount -= delta
		if stat.FileCount <= 0 {
			return tx.Delete(&ArchiveStat{Source: source}).Error
		}
		return tx.Model(&stat).Updates(map[string]interface{}{
			"file_count": stat.FileCount,
			"updated_at": time.Now(),
		}).Error
	})
}

func buildArchiveStatsSnapshot(stats []ArchiveStat) *ArchiveStatsSnapshot {
	items := make([]ArchiveStatItem, 0, len(stats))
	totalFiles := 0
	for _, stat := range stats {
		if stat.FileCount < 0 {
			continue
		}
		totalFiles += stat.FileCount
		items = append(items, ArchiveStatItem{
			Source:    stat.Source,
			FileCount: stat.FileCount,
		})
	}
	return &ArchiveStatsSnapshot{
		TotalFiles: totalFiles,
		Sources:    items,
	}
}

func (row *ArchiveDocument) BeforeCreate(tx *gorm.DB) error {
	if row.MaterialID != 0 {
		return nil
	}
	identity := strings.TrimSpace(row.SourceURL)
	if identity == "" {
		identity = "archive:" + row.Domain + "/" + row.FileName
	}
	var existing ArchiveDocument
	result := tx.Where("domain = ? AND file_name = ?", row.Domain, row.FileName).Limit(1).Find(&existing)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 && existing.MaterialID != 0 {
		if normalized, err := material.NormalizeURL(identity); err == nil {
			identity = normalized
		}
		id, err := material.BindIdentity(tx, existing.MaterialID, "url", identity)
		row.MaterialID = id
		return err
	}
	record, err := material.EnsureURL(tx, identity, row.Title, row.Summary)
	row.MaterialID = record.ID
	return err
}
