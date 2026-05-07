package common

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const temporaryArchiveDirName = "Temporary"

// RefreshArchiveStatsFromDisk 根据归档目录重建数据库统计信息。
func RefreshArchiveStatsFromDisk() (*ArchiveStatsSnapshot, error) {
	stats, err := ScanArchiveStats(ARCHIVEFILELOACTION)
	if err != nil {
		return nil, err
	}

	return ReplaceArchiveStats(stats)
}

// ScanArchiveStats 将归档根目录下的一级目录视为 URL 来源，并统计其下的 HTML 文件。
func ScanArchiveStats(rootDir string) ([]ArchiveStat, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ArchiveStat{}, nil
		}
		return nil, err
	}

	stats := make([]ArchiveStat, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		source := strings.TrimSpace(entry.Name())
		if source == "" || strings.EqualFold(source, temporaryArchiveDirName) {
			continue
		}

		// 文件保存后会移动到来源目录，刷新统计时只按这些稳定目录重新计算。
		fileCount, err := countHTMLFiles(filepath.Join(rootDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if fileCount == 0 {
			continue
		}

		stats = append(stats, ArchiveStat{
			Source:    source,
			FileCount: fileCount,
		})
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Source < stats[j].Source
	})

	return stats, nil
}

// countHTMLFiles 递归统计来源目录中的 HTML 文件，因为归档页面可能包含嵌套目录。
func countHTMLFiles(rootDir string) (int, error) {
	fileCount := 0
	err := filepath.WalkDir(rootDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if isHTMLArchiveFile(path) {
			fileCount++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	return fileCount, nil
}

func isHTMLArchiveFile(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".html" || extension == ".htm"
}
