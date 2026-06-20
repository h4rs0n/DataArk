package common

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type KeywordStat struct {
	Keyword        string `json:"keyword"`
	Count          int64  `json:"count"`
	ResultCount    int    `json:"resultCount"`
	LastSearchedAt string `json:"lastSearchedAt"`
}

type ArchiveRankingItem struct {
	Path          string `json:"path"`
	Domain        string `json:"domain"`
	FileName      string `json:"fileName"`
	Title         string `json:"title"`
	Summary       string `json:"summary"`
	SourceURL     string `json:"sourceUrl"`
	ClickCount    int64  `json:"clickCount"`
	LastClickedAt string `json:"lastClickedAt"`
}

type ArchiveRecommendationItem struct {
	Path       string    `json:"path"`
	Domain     string    `json:"domain"`
	FileName   string    `json:"fileName"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
	SourceURL  string    `json:"sourceUrl"`
	Score      float64   `json:"score"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Reason     string    `json:"reason"`
	ClickCount int64     `json:"clickCount"`
}

func NormalizeSearchKeyword(keyword string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(keyword)), " ")
}

func RecordSearchEvent(keyword string, resultCount int) error {
	keyword = NormalizeSearchKeyword(keyword)
	if db == nil || keyword == "" {
		return nil
	}
	if resultCount < 0 {
		resultCount = 0
	}
	return db.Create(&SearchEvent{Keyword: keyword, ResultCount: resultCount}).Error
}

func GetKeywordStats(prefix string, window string, limit int) ([]KeywordStat, error) {
	if db == nil {
		return []KeywordStat{}, nil
	}
	limit = normalizeLimit(limit, 10, 50)
	prefix = strings.ToLower(NormalizeSearchKeyword(prefix))
	stats := make([]KeywordStat, 0)

	query := db.Model(&SearchEvent{}).
		Select("keyword, COUNT(*) AS count, MAX(result_count) AS result_count, MAX(created_at) AS last_searched_at")
	if since, ok := windowStart(window); ok {
		query = query.Where("created_at >= ?", since)
	}
	if prefix != "" {
		query = query.Where("LOWER(keyword) LIKE ?", prefix+"%")
	}

	if err := query.Group("keyword").Order("count desc, last_searched_at desc, keyword asc").Limit(limit).Scan(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

func RecordArchiveClick(rawPath string, keyword string) (*ArchiveClickEvent, error) {
	archivePath, err := ResolveArchiveDocumentPath(rawPath)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(archivePath.AbsPath); err != nil {
		return nil, err
	} else if info.IsDir() {
		return nil, errors.New("archive path points to a directory")
	}

	event := &ArchiveClickEvent{
		Domain:   archivePath.Domain,
		FileName: archivePath.Filename,
		Path:     archivePath.RequestPath,
		Keyword:  NormalizeSearchKeyword(keyword),
	}
	if db == nil {
		return event, nil
	}
	if err := db.Create(event).Error; err != nil {
		return nil, err
	}
	return event, nil
}

func GetArchiveRankings(window string, limit int) ([]ArchiveRankingItem, error) {
	if db == nil {
		return []ArchiveRankingItem{}, nil
	}
	limit = normalizeLimit(limit, 20, 100)
	rows := make([]ArchiveRankingItem, 0)

	query := db.Model(&ArchiveClickEvent{}).
		Select("domain, file_name, path, COUNT(*) AS click_count, MAX(created_at) AS last_clicked_at")
	if since, ok := windowStart(window); ok {
		query = query.Where("created_at >= ?", since)
	}

	if err := query.Group("domain, file_name, path").Order("click_count desc, last_clicked_at desc").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}

	for index := range rows {
		if document, err := GetArchiveDocument(rows[index].Domain, rows[index].FileName); err == nil {
			rows[index].Title = archiveDisplayTitle(document)
			rows[index].Summary = document.Summary
			rows[index].SourceURL = document.SourceURL
		}
	}
	return rows, nil
}

func GetArchiveRecommendations(window string, limit int) ([]ArchiveRecommendationItem, error) {
	if db == nil {
		return []ArchiveRecommendationItem{}, nil
	}
	limit = normalizeLimit(limit, 20, 100)

	var documents []ArchiveDocument
	if err := db.Order("updated_at desc").Limit(500).Find(&documents).Error; err != nil {
		return nil, err
	}
	if len(documents) == 0 {
		return []ArchiveRecommendationItem{}, nil
	}

	clicks, err := archiveClickCounts(window)
	if err != nil {
		return nil, err
	}
	keywords, err := GetKeywordStats("", window, 20)
	if err != nil {
		return nil, err
	}

	recommendations := make([]ArchiveRecommendationItem, 0, len(documents))
	for _, document := range documents {
		path := archiveRequestPath(document.Domain, document.FileName)
		clickCount := clicks[document.Domain+"/"+document.FileName]
		text := strings.ToLower(document.Title + " " + document.Summary + " " + document.Domain + " " + document.SourceURL)
		score := float64(clickCount) * 5
		reason := "近期归档"
		if clickCount > 0 {
			reason = "近期常看"
		}

		for _, keyword := range keywords {
			normalizedKeyword := strings.ToLower(keyword.Keyword)
			if normalizedKeyword != "" && strings.Contains(text, normalizedKeyword) {
				score += float64(keyword.Count) * 3
				if reason == "近期归档" {
					reason = "匹配常搜关键词"
				}
			}
		}
		score += recencyScore(document.UpdatedAt)

		recommendations = append(recommendations, ArchiveRecommendationItem{
			Path:       path,
			Domain:     document.Domain,
			FileName:   document.FileName,
			Title:      archiveDisplayTitle(&document),
			Summary:    document.Summary,
			SourceURL:  document.SourceURL,
			Score:      score,
			UpdatedAt:  document.UpdatedAt,
			Reason:     reason,
			ClickCount: clickCount,
		})
	}

	sort.SliceStable(recommendations, func(i, j int) bool {
		if recommendations[i].Score != recommendations[j].Score {
			return recommendations[i].Score > recommendations[j].Score
		}
		return recommendations[i].UpdatedAt.After(recommendations[j].UpdatedAt)
	})
	if len(recommendations) > limit {
		recommendations = recommendations[:limit]
	}
	return recommendations, nil
}

func BackfillArchiveDocumentMetadataFromDisk() error {
	if db == nil {
		return nil
	}
	documents, err := scanArchiveDocumentsFromDisk()
	if err != nil {
		return err
	}
	for _, document := range documents {
		existingSourceURL := document.SourceURL
		if existing, err := GetArchiveDocument(document.Domain, document.FileName); err == nil {
			existingSourceURL = existing.SourceURL
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := SaveArchiveDocumentDetails(document.Domain, document.FileName, existingSourceURL, document.Title, document.Summary); err != nil {
			return err
		}
	}
	return nil
}

func scanArchiveDocumentsFromDisk() ([]ArchiveDocument, error) {
	stats, err := ScanArchiveStats(ARCHIVEFILELOACTION)
	if err != nil {
		return nil, err
	}
	documents := make([]ArchiveDocument, 0)
	for _, stat := range stats {
		sourceDir := stat.Source
		root := strings.TrimRight(ARCHIVEFILELOACTION, string(os.PathSeparator)) + string(os.PathSeparator) + sourceDir
		err := filepathWalkHTML(root, func(absPath string, relativeName string) error {
			content, err := GetHTMLFileContent(absPath)
			if err != nil {
				return err
			}
			title, _ := GetHTMLTitle(content)
			text, _ := ExtractHTMLText(content)
			documents = append(documents, ArchiveDocument{
				Domain:   sourceDir,
				FileName: relativeName,
				Title:    title,
				Summary:  BuildSummary(text, 220),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return documents, nil
}

func filepathWalkHTML(root string, visit func(absPath string, relativeName string) error) error {
	return filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isHTMLArchiveFile(currentPath) {
			return nil
		}
		relativePath, err := filepath.Rel(root, currentPath)
		if err != nil {
			return err
		}
		return visit(currentPath, filepath.ToSlash(relativePath))
	})
}

func archiveClickCounts(window string) (map[string]int64, error) {
	query := db.Model(&ArchiveClickEvent{}).Select("domain, file_name, COUNT(*) AS click_count")
	if since, ok := windowStart(window); ok {
		query = query.Where("created_at >= ?", since)
	}
	var rows []struct {
		Domain     string
		FileName   string
		ClickCount int64
	}
	if err := query.Group("domain, file_name").Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Domain+"/"+row.FileName] = row.ClickCount
	}
	return counts, nil
}

func windowStart(window string) (time.Time, bool) {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "", "7d", "week":
		return time.Now().AddDate(0, 0, -7), true
	case "30d", "month":
		return time.Now().AddDate(0, 0, -30), true
	case "all", "total":
		return time.Time{}, false
	default:
		return time.Now().AddDate(0, 0, -7), true
	}
}

func normalizeLimit(limit int, defaultLimit int, maxLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func archiveDisplayTitle(document *ArchiveDocument) string {
	title := strings.TrimSpace(document.Title)
	if title != "" {
		return title
	}
	return document.FileName
}

func archiveRequestPath(domain string, fileName string) string {
	return "/archive/" + strings.Trim(domain, "/") + "/" + strings.TrimLeft(fileName, "/")
}

func recencyScore(updatedAt time.Time) float64 {
	if updatedAt.IsZero() {
		return 0
	}
	ageHours := time.Since(updatedAt).Hours()
	switch {
	case ageHours < 24:
		return 4
	case ageHours < 24*7:
		return 2
	case ageHours < 24*30:
		return 1
	default:
		return 0
	}
}

func BuildSummary(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}
