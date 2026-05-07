package search

import (
	"DataArk/common"
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const rebuildBatchSize = 100

type RebuildIndexResult struct {
	Documents int `json:"documents"`
}

func RebuildIndexFromArchive(ctx context.Context) (*RebuildIndexResult, error) {
	result, _, err := rebuildIndexFromArchive(ctx, false)
	return result, err
}

func RebuildRecoverableIndexFromArchive(ctx context.Context) (*RebuildIndexResult, []ArchiveConsistencyIssue, error) {
	return rebuildIndexFromArchive(ctx, true)
}

func rebuildIndexFromArchive(ctx context.Context, skipInvalidFiles bool) (*RebuildIndexResult, []ArchiveConsistencyIssue, error) {
	client := meilisearch.New(common.MEILIHOST, meilisearch.WithAPIKey(common.MEILIAPIKey))
	if err := recreateBlogsIndex(ctx, client); err != nil {
		return nil, nil, err
	}

	archiveRoot := filepath.Clean(common.ARCHIVEFILELOACTION)
	documents := make([]map[string]interface{}, 0, rebuildBatchSize)
	indexedDocuments := 0
	unrecoverableIssues := make([]ArchiveConsistencyIssue, 0)

	flush := func() error {
		if len(documents) == 0 {
			return nil
		}

		taskInfo, err := client.Index(common.MEILIBlogsIndex).AddDocumentsWithContext(ctx, documents)
		if err != nil {
			return err
		}
		task, err := client.WaitForTaskWithContext(ctx, taskInfo.TaskUID, time.Second)
		if err != nil {
			return err
		}
		if task.Status != meilisearch.TaskStatusSucceeded {
			return fmt.Errorf("meilisearch add documents task %d finished with status %s", taskInfo.TaskUID, task.Status)
		}

		indexedDocuments += len(documents)
		documents = documents[:0]
		return nil
	}

	if _, err := os.Stat(archiveRoot); err != nil {
		if os.IsNotExist(err) {
			return &RebuildIndexResult{}, unrecoverableIssues, nil
		}
		return nil, nil, err
	}

	err := filepath.WalkDir(archiveRoot, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".html" && extension != ".htm" {
			return nil
		}

		relativePath, err := filepath.Rel(archiveRoot, currentPath)
		if err != nil {
			return err
		}
		pathParts := strings.Split(filepath.ToSlash(relativePath), "/")
		if len(pathParts) < 2 || strings.EqualFold(pathParts[0], "Temporary") {
			return nil
		}

		fileName := strings.Join(pathParts[1:], "/")
		document, err := buildDocumentFromHTML(currentPath, pathParts[0], fileName)
		if err != nil {
			if !skipInvalidFiles {
				return err
			}
			unrecoverableIssues = append(unrecoverableIssues, ArchiveConsistencyIssue{
				Severity:    ArchiveConsistencySeverityError,
				Store:       ArchiveConsistencyStoreHTML,
				Domain:      pathParts[0],
				Filename:    fileName,
				Path:        "/" + path.Join("archive", pathParts[0], fileName),
				Message:     fmt.Sprintf("HTML 文件存在但无法解析为搜索文档: %v", err),
				Recoverable: false,
			})
			return nil
		}
		documents = append(documents, document)

		if len(documents) >= rebuildBatchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	if err := flush(); err != nil {
		return nil, nil, err
	}

	return &RebuildIndexResult{Documents: indexedDocuments}, unrecoverableIssues, nil
}

func recreateBlogsIndex(ctx context.Context, client meilisearch.ServiceManager) error {
	indexes, err := client.ListIndexesWithContext(ctx, nil)
	if err != nil {
		return err
	}

	for _, index := range indexes.Results {
		if index.UID != common.MEILIBlogsIndex {
			continue
		}
		taskInfo, err := client.DeleteIndexWithContext(ctx, common.MEILIBlogsIndex)
		if err != nil {
			return err
		}
		if err := waitForServiceTask(ctx, client, taskInfo); err != nil {
			return err
		}
		break
	}

	taskInfo, err := client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{
		Uid:        common.MEILIBlogsIndex,
		PrimaryKey: "id",
	})
	if err != nil {
		return err
	}
	return waitForServiceTask(ctx, client, taskInfo)
}

func waitForServiceTask(ctx context.Context, client meilisearch.ServiceManager, taskInfo *meilisearch.TaskInfo) error {
	task, err := client.WaitForTaskWithContext(ctx, taskInfo.TaskUID, time.Second)
	if err != nil {
		return err
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return fmt.Errorf("meilisearch task %d finished with status %s", taskInfo.TaskUID, task.Status)
	}
	return nil
}

func buildDocumentFromHTML(htmlPath string, domain string, fileName string) (map[string]interface{}, error) {
	htmlContent, err := common.GetHTMLFileContent(htmlPath)
	if err != nil {
		return nil, err
	}
	title, err := common.GetHTMLTitle(htmlContent)
	if err != nil {
		return nil, err
	}
	pureText, err := common.ExtractHTMLText(htmlContent)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":       uuid.New().String(),
		"title":    title,
		"filename": fileName,
		"domain":   domain,
		"content":  pureText,
	}, nil
}
