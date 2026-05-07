package search

import (
	"DataArk/common"
	"context"
	"fmt"
	"github.com/meilisearch/meilisearch-go"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ArchiveConsistencySeverityWarning = "warning"
	ArchiveConsistencySeverityError   = "error"
)

const (
	ArchiveConsistencyStoreHTML     = "html"
	ArchiveConsistencyStoreMeili    = "meilisearch"
	ArchiveConsistencyStoreDatabase = "database"
)

const archiveConsistencyDocumentPageSize = 1000

type ArchiveConsistencyIssue struct {
	Severity    string   `json:"severity"`
	Store       string   `json:"store"`
	Domain      string   `json:"domain"`
	Filename    string   `json:"filename"`
	Path        string   `json:"path"`
	DocumentIDs []string `json:"documentIds"`
	Message     string   `json:"message"`
	Recoverable bool     `json:"recoverable"`
}

type ArchiveConsistencyReport struct {
	CheckedAt            string                    `json:"checkedAt"`
	Consistent           bool                      `json:"consistent"`
	HTMLFiles            int                       `json:"htmlFiles"`
	MeiliDocuments       int                       `json:"meiliDocuments"`
	DatabaseStatTotal    int                       `json:"databaseStatTotal"`
	DiskSources          []common.ArchiveStatItem  `json:"diskSources"`
	DatabaseSources      []common.ArchiveStatItem  `json:"databaseSources"`
	RecoverableIssues    []ArchiveConsistencyIssue `json:"recoverableIssues"`
	UnrecoverableIssues  []ArchiveConsistencyIssue `json:"unrecoverableIssues"`
	Actions              []string                  `json:"actions"`
	IndexedDocuments     int                       `json:"indexedDocuments"`
	RefreshedStatSources int                       `json:"refreshedStatSources"`
}

type archiveHTMLFile struct {
	Domain      string
	Filename    string
	RequestPath string
	AbsPath     string
}

type archiveIndexDocument struct {
	ID       string
	Domain   string
	Filename string
}

type archiveIndexStore interface {
	ListArchiveDocuments(ctx context.Context) ([]archiveIndexDocument, error)
	RebuildArchiveIndex(ctx context.Context) (int, []ArchiveConsistencyIssue, error)
}

type archiveStatsStore interface {
	GetArchiveStats() (*common.ArchiveStatsSnapshot, error)
	RefreshArchiveStats() (*common.ArchiveStatsSnapshot, error)
}

type archiveConsistencyService struct {
	archiveRoot string
	index       archiveIndexStore
	stats       archiveStatsStore
	now         func() time.Time
}

type meiliArchiveIndexStore struct{}

type commonArchiveStatsStore struct{}

func CheckArchiveConsistency(ctx context.Context) (*ArchiveConsistencyReport, error) {
	return newArchiveConsistencyService().Check(ctx)
}

func RepairArchiveConsistency(ctx context.Context) (*ArchiveConsistencyReport, error) {
	return newArchiveConsistencyService().Repair(ctx)
}

func newArchiveConsistencyService() archiveConsistencyService {
	return archiveConsistencyService{
		archiveRoot: common.ARCHIVEFILELOACTION,
		index:       meiliArchiveIndexStore{},
		stats:       commonArchiveStatsStore{},
		now:         time.Now,
	}
}

func (s archiveConsistencyService) Check(ctx context.Context) (*ArchiveConsistencyReport, error) {
	if s.now == nil {
		s.now = time.Now
	}

	files, diskStats, parseIssues, err := scanArchiveHTMLFiles(s.archiveRoot)
	if err != nil {
		return nil, err
	}
	documents, err := s.index.ListArchiveDocuments(ctx)
	if err != nil {
		return nil, err
	}
	databaseStats, err := s.stats.GetArchiveStats()
	if err != nil {
		return nil, err
	}
	if databaseStats == nil {
		databaseStats = &common.ArchiveStatsSnapshot{}
	}

	report := &ArchiveConsistencyReport{
		CheckedAt:         s.now().Format(time.RFC3339),
		HTMLFiles:         len(files),
		MeiliDocuments:    len(documents),
		DatabaseStatTotal: databaseStats.TotalFiles,
		DiskSources:       archiveStatItems(diskStats),
		DatabaseSources:   databaseStats.Sources,
		Actions:           []string{},
	}

	compareArchiveFilesAndIndex(report, files, documents, parseIssues)
	compareArchiveStats(report, diskStats, databaseStats)
	finalizeArchiveConsistencyReport(report)
	return report, nil
}

func (s archiveConsistencyService) Repair(ctx context.Context) (*ArchiveConsistencyReport, error) {
	initialReport, err := s.Check(ctx)
	if err != nil {
		return nil, err
	}
	carriedUnrecoverable := append([]ArchiveConsistencyIssue(nil), initialReport.UnrecoverableIssues...)

	indexedDocuments, rebuildIssues, err := s.index.RebuildArchiveIndex(ctx)
	if err != nil {
		return nil, err
	}

	refreshedStats, err := s.stats.RefreshArchiveStats()
	if err != nil {
		return nil, err
	}
	if refreshedStats == nil {
		refreshedStats = &common.ArchiveStatsSnapshot{}
	}

	report, err := s.Check(ctx)
	if err != nil {
		return nil, err
	}
	report.Actions = []string{
		fmt.Sprintf("已根据现存 HTML 文件重建 Meilisearch 索引，写入 %d 条文档", indexedDocuments),
		fmt.Sprintf("已根据现存 HTML 文件刷新数据库统计，来源数 %d 个", len(refreshedStats.Sources)),
	}
	report.IndexedDocuments = indexedDocuments
	report.RefreshedStatSources = len(refreshedStats.Sources)
	report.UnrecoverableIssues = mergeArchiveConsistencyIssues(report.UnrecoverableIssues, carriedUnrecoverable, rebuildIssues)
	finalizeArchiveConsistencyReport(report)
	return report, nil
}

func (meiliArchiveIndexStore) ListArchiveDocuments(ctx context.Context) ([]archiveIndexDocument, error) {
	client := meilisearch.New(common.MEILIHOST, meilisearch.WithAPIKey(common.MEILIAPIKey))
	index := client.Index(common.MEILIBlogsIndex)
	documents := make([]archiveIndexDocument, 0)

	for offset := int64(0); ; {
		var result meilisearch.DocumentsResult
		err := index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
			Limit:  archiveConsistencyDocumentPageSize,
			Offset: offset,
			Fields: []string{"id", "domain", "filename"},
		}, &result)
		if err != nil {
			return nil, err
		}

		for _, document := range result.Results {
			documents = append(documents, archiveIndexDocument{
				ID:       documentString(document, "id"),
				Domain:   documentString(document, "domain"),
				Filename: documentString(document, "filename"),
			})
		}
		if len(result.Results) < archiveConsistencyDocumentPageSize {
			break
		}
		offset += int64(len(result.Results))
	}

	return documents, nil
}

func (meiliArchiveIndexStore) RebuildArchiveIndex(ctx context.Context) (int, []ArchiveConsistencyIssue, error) {
	result, issues, err := RebuildRecoverableIndexFromArchive(ctx)
	if err != nil {
		return 0, nil, err
	}
	if result == nil {
		return 0, issues, nil
	}
	return result.Documents, issues, nil
}

func (commonArchiveStatsStore) GetArchiveStats() (*common.ArchiveStatsSnapshot, error) {
	return common.GetArchiveStats()
}

func (commonArchiveStatsStore) RefreshArchiveStats() (*common.ArchiveStatsSnapshot, error) {
	return common.RefreshArchiveStatsFromDisk()
}

func scanArchiveHTMLFiles(rootDir string) ([]archiveHTMLFile, []common.ArchiveStat, []ArchiveConsistencyIssue, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, nil, nil, fmt.Errorf("archive location is empty")
	}

	archiveRoot := filepath.Clean(rootDir)
	if _, err := os.Stat(archiveRoot); err != nil {
		if os.IsNotExist(err) {
			return []archiveHTMLFile{}, []common.ArchiveStat{}, []ArchiveConsistencyIssue{}, nil
		}
		return nil, nil, nil, err
	}

	files := make([]archiveHTMLFile, 0)
	parseIssues := make([]ArchiveConsistencyIssue, 0)
	countByDomain := make(map[string]int)

	err := filepath.WalkDir(archiveRoot, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if isTemporaryArchiveDir(archiveRoot, currentPath) {
				return filepath.SkipDir
			}
			return nil
		}

		if !isArchiveHTMLFile(entry.Name()) {
			return nil
		}

		relativePath, err := filepath.Rel(archiveRoot, currentPath)
		if err != nil {
			return err
		}
		pathParts := strings.Split(filepath.ToSlash(relativePath), "/")
		if len(pathParts) < 2 || strings.EqualFold(pathParts[0], "Temporary") {
			parseIssues = append(parseIssues, ArchiveConsistencyIssue{
				Severity:    ArchiveConsistencySeverityError,
				Store:       ArchiveConsistencyStoreHTML,
				Path:        "/" + path.Join("archive", filepath.ToSlash(relativePath)),
				Message:     "HTML 文件不在 archive/{domain}/{filename} 结构内，无法自动归属到来源域名",
				Recoverable: false,
			})
			return nil
		}

		domain := strings.TrimSpace(pathParts[0])
		fileName := strings.Join(pathParts[1:], "/")
		requestPath := "/" + path.Join("archive", domain, fileName)
		file := archiveHTMLFile{
			Domain:      domain,
			Filename:    fileName,
			RequestPath: requestPath,
			AbsPath:     currentPath,
		}
		files = append(files, file)
		countByDomain[domain]++

		if _, err := buildDocumentFromHTML(currentPath, domain, fileName); err != nil {
			parseIssues = append(parseIssues, ArchiveConsistencyIssue{
				Severity:    ArchiveConsistencySeverityError,
				Store:       ArchiveConsistencyStoreHTML,
				Domain:      domain,
				Filename:    fileName,
				Path:        requestPath,
				Message:     fmt.Sprintf("HTML 文件存在但无法解析为搜索文档: %v", err),
				Recoverable: false,
			})
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return archiveIdentityKey(files[i].Domain, files[i].Filename) < archiveIdentityKey(files[j].Domain, files[j].Filename)
	})

	stats := make([]common.ArchiveStat, 0, len(countByDomain))
	for domain, count := range countByDomain {
		stats = append(stats, common.ArchiveStat{Source: domain, FileCount: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Source < stats[j].Source
	})
	sortArchiveConsistencyIssues(parseIssues)

	return files, stats, parseIssues, nil
}

func compareArchiveFilesAndIndex(report *ArchiveConsistencyReport, files []archiveHTMLFile, documents []archiveIndexDocument, parseIssues []ArchiveConsistencyIssue) {
	fileByKey := make(map[string]archiveHTMLFile, len(files))
	parseIssueByKey := make(map[string]bool, len(parseIssues))
	documentsByKey := make(map[string][]archiveIndexDocument)

	for _, file := range files {
		fileByKey[archiveIdentityKey(file.Domain, file.Filename)] = file
	}
	for _, issue := range parseIssues {
		if issue.Domain != "" && issue.Filename != "" {
			parseIssueByKey[archiveIdentityKey(issue.Domain, issue.Filename)] = true
		}
		report.UnrecoverableIssues = append(report.UnrecoverableIssues, issue)
	}

	for _, document := range documents {
		if strings.TrimSpace(document.Domain) == "" || strings.TrimSpace(document.Filename) == "" {
			report.RecoverableIssues = append(report.RecoverableIssues, ArchiveConsistencyIssue{
				Severity:    ArchiveConsistencySeverityWarning,
				Store:       ArchiveConsistencyStoreMeili,
				DocumentIDs: []string{document.ID},
				Message:     "Meilisearch 文档缺少 domain 或 filename 字段，可通过重建索引清理",
				Recoverable: true,
			})
			continue
		}
		key := archiveIdentityKey(document.Domain, document.Filename)
		documentsByKey[key] = append(documentsByKey[key], document)
	}

	for _, file := range files {
		key := archiveIdentityKey(file.Domain, file.Filename)
		matchingDocuments := documentsByKey[key]
		if len(matchingDocuments) == 0 {
			if parseIssueByKey[key] {
				continue
			}
			report.RecoverableIssues = append(report.RecoverableIssues, ArchiveConsistencyIssue{
				Severity:    ArchiveConsistencySeverityWarning,
				Store:       ArchiveConsistencyStoreMeili,
				Domain:      file.Domain,
				Filename:    file.Filename,
				Path:        file.RequestPath,
				Message:     "HTML 文件存在但搜索索引缺失，可通过重建索引恢复",
				Recoverable: true,
			})
			continue
		}
		if len(matchingDocuments) > 1 {
			report.RecoverableIssues = append(report.RecoverableIssues, ArchiveConsistencyIssue{
				Severity:    ArchiveConsistencySeverityWarning,
				Store:       ArchiveConsistencyStoreMeili,
				Domain:      file.Domain,
				Filename:    file.Filename,
				Path:        file.RequestPath,
				DocumentIDs: archiveDocumentIDs(matchingDocuments),
				Message:     "同一个 HTML 文件存在多条搜索索引记录，可通过重建索引去重",
				Recoverable: true,
			})
		}
	}

	for key, matchingDocuments := range documentsByKey {
		if _, ok := fileByKey[key]; ok {
			continue
		}
		domain, filename := splitArchiveIdentityKey(key)
		report.UnrecoverableIssues = append(report.UnrecoverableIssues, ArchiveConsistencyIssue{
			Severity:    ArchiveConsistencySeverityError,
			Store:       ArchiveConsistencyStoreMeili,
			Domain:      domain,
			Filename:    filename,
			Path:        "/" + path.Join("archive", domain, filename),
			DocumentIDs: archiveDocumentIDs(matchingDocuments),
			Message:     "搜索索引指向的 HTML 文件不存在，索引可清理但原始归档内容无法自动恢复",
			Recoverable: false,
		})
	}
}

func compareArchiveStats(report *ArchiveConsistencyReport, diskStats []common.ArchiveStat, databaseStats *common.ArchiveStatsSnapshot) {
	diskBySource := make(map[string]int, len(diskStats))
	databaseBySource := make(map[string]int, len(databaseStats.Sources))

	for _, stat := range diskStats {
		diskBySource[stat.Source] = stat.FileCount
	}
	for _, stat := range databaseStats.Sources {
		databaseBySource[stat.Source] = stat.FileCount
	}

	if databaseStats.TotalFiles != report.HTMLFiles {
		report.RecoverableIssues = append(report.RecoverableIssues, ArchiveConsistencyIssue{
			Severity:    ArchiveConsistencySeverityWarning,
			Store:       ArchiveConsistencyStoreDatabase,
			Message:     fmt.Sprintf("数据库统计总数为 %d，磁盘 HTML 文件数为 %d，可通过刷新统计恢复", databaseStats.TotalFiles, report.HTMLFiles),
			Recoverable: true,
		})
	}

	for source, fileCount := range diskBySource {
		if databaseBySource[source] != fileCount {
			report.RecoverableIssues = append(report.RecoverableIssues, ArchiveConsistencyIssue{
				Severity:    ArchiveConsistencySeverityWarning,
				Store:       ArchiveConsistencyStoreDatabase,
				Domain:      source,
				Message:     fmt.Sprintf("来源 %s 的数据库统计为 %d，磁盘文件数为 %d，可通过刷新统计恢复", source, databaseBySource[source], fileCount),
				Recoverable: true,
			})
		}
	}
	for source, fileCount := range databaseBySource {
		if _, ok := diskBySource[source]; ok {
			continue
		}
		report.RecoverableIssues = append(report.RecoverableIssues, ArchiveConsistencyIssue{
			Severity:    ArchiveConsistencySeverityWarning,
			Store:       ArchiveConsistencyStoreDatabase,
			Domain:      source,
			Message:     fmt.Sprintf("数据库统计中存在来源 %s（%d 个文件），但磁盘已无对应 HTML 文件，可通过刷新统计清理", source, fileCount),
			Recoverable: true,
		})
	}
}

func finalizeArchiveConsistencyReport(report *ArchiveConsistencyReport) {
	report.RecoverableIssues = mergeArchiveConsistencyIssues(report.RecoverableIssues)
	report.UnrecoverableIssues = mergeArchiveConsistencyIssues(report.UnrecoverableIssues)
	sortArchiveConsistencyIssues(report.RecoverableIssues)
	sortArchiveConsistencyIssues(report.UnrecoverableIssues)
	report.Consistent = len(report.RecoverableIssues) == 0 && len(report.UnrecoverableIssues) == 0
}

func mergeArchiveConsistencyIssues(issueGroups ...[]ArchiveConsistencyIssue) []ArchiveConsistencyIssue {
	merged := make([]ArchiveConsistencyIssue, 0)
	seen := make(map[string]bool)
	for _, issues := range issueGroups {
		for _, issue := range issues {
			key := strings.Join([]string{
				issue.Severity,
				issue.Store,
				issue.Domain,
				issue.Filename,
				issue.Path,
				strings.Join(issue.DocumentIDs, ","),
				issue.Message,
			}, "\x00")
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, issue)
		}
	}
	return merged
}

func archiveStatItems(stats []common.ArchiveStat) []common.ArchiveStatItem {
	items := make([]common.ArchiveStatItem, 0, len(stats))
	for _, stat := range stats {
		items = append(items, common.ArchiveStatItem{
			Source:    stat.Source,
			FileCount: stat.FileCount,
		})
	}
	return items
}

func archiveDocumentIDs(documents []archiveIndexDocument) []string {
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		if document.ID != "" {
			ids = append(ids, document.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func sortArchiveConsistencyIssues(issues []ArchiveConsistencyIssue) {
	sort.Slice(issues, func(i, j int) bool {
		left := archiveConsistencyIssueSortKey(issues[i])
		right := archiveConsistencyIssueSortKey(issues[j])
		return left < right
	})
}

func archiveConsistencyIssueSortKey(issue ArchiveConsistencyIssue) string {
	return strings.Join([]string{
		issue.Store,
		issue.Domain,
		issue.Filename,
		issue.Path,
		issue.Message,
	}, "\x00")
}

func archiveIdentityKey(domain string, filename string) string {
	return strings.TrimSpace(domain) + "\x00" + strings.TrimSpace(filename)
}

func splitArchiveIdentityKey(key string) (string, string) {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return key, ""
	}
	return parts[0], parts[1]
}

func isTemporaryArchiveDir(root string, currentPath string) bool {
	relativePath, err := filepath.Rel(root, currentPath)
	if err != nil || relativePath == "." {
		return false
	}
	pathParts := strings.Split(filepath.ToSlash(relativePath), "/")
	return len(pathParts) == 1 && strings.EqualFold(pathParts[0], "Temporary")
}

func isArchiveHTMLFile(fileName string) bool {
	extension := strings.ToLower(filepath.Ext(fileName))
	return extension == ".html" || extension == ".htm"
}
