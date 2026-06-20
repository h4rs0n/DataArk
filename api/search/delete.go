package search

import (
	"DataArk/common"
	"context"
	"errors"
	"fmt"
	"github.com/meilisearch/meilisearch-go"
	"os"
	"time"
)

const (
	deleteDocumentLookupLimit = 1000
	deleteDocumentTaskTimeout = 10 * time.Second
)

var (
	ErrInvalidArchivePath      = errors.New("invalid archive html path")
	ErrArchiveDocumentNotFound = errors.New("archive document not found")
	ErrArchiveFileNotFound     = errors.New("archive html file not found")
)

type DeleteDocResult struct {
	Path        string   `json:"path"`
	Domain      string   `json:"domain"`
	Filename    string   `json:"filename"`
	DocumentIDs []string `json:"documentIds"`
	TaskUID     int64    `json:"taskUid"`
}

func DeleteDocByHTMLPath(ctx context.Context, rawPath string) (*DeleteDocResult, error) {
	archivePath, err := common.ResolveArchiveDocumentPath(rawPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchivePath, err)
	}

	fileInfo, err := os.Stat(archivePath.AbsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrArchiveFileNotFound, archivePath.RequestPath)
		}
		return nil, err
	}
	if fileInfo.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrInvalidArchivePath, archivePath.RequestPath)
	}

	client := meilisearch.New(common.MEILIHOST, meilisearch.WithAPIKey(common.MEILIAPIKey))
	index := client.Index(common.MEILIBlogsIndex)

	documentIDs, err := findArchiveDocumentIDs(index, archivePath.Domain, archivePath.Filename)
	if err != nil {
		return nil, err
	}
	if len(documentIDs) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrArchiveDocumentNotFound, archivePath.RequestPath)
	}

	taskInfo, err := deleteArchiveDocuments(index, documentIDs)
	if err != nil {
		return nil, err
	}

	waitCtx, cancel := context.WithTimeout(ctx, deleteDocumentTaskTimeout)
	defer cancel()
	task, err := client.WaitForTaskWithContext(waitCtx, taskInfo.TaskUID, 0)
	if err != nil {
		return nil, err
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return nil, fmt.Errorf("meilisearch delete task %d finished with status %s", task.TaskUID, task.Status)
	}

	if err := os.Remove(archivePath.AbsPath); err != nil {
		return nil, err
	}
	if err := common.DeleteArchiveDocumentMetadata(archivePath.Domain, archivePath.Filename); err != nil {
		return nil, err
	}
	if err := common.DecrementArchiveStat(archivePath.Domain, 1); err != nil {
		return nil, err
	}

	return &DeleteDocResult{
		Path:        archivePath.RequestPath,
		Domain:      archivePath.Domain,
		Filename:    archivePath.Filename,
		DocumentIDs: documentIDs,
		TaskUID:     taskInfo.TaskUID,
	}, nil
}

func resolveArchiveDocumentPath(rawPath string) (*common.ArchiveDocumentPath, error) {
	archivePath, err := common.ResolveArchiveDocumentPath(rawPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArchivePath, err)
	}
	return archivePath, nil
}

func findArchiveDocumentIDs(index meilisearch.DocumentManager, domain string, filename string) ([]string, error) {
	documentIDs := make([]string, 0, 1)
	for offset := int64(0); ; {
		var documents meilisearch.DocumentsResult
		err := index.GetDocuments(&meilisearch.DocumentsQuery{
			Limit:  deleteDocumentLookupLimit,
			Offset: offset,
			Fields: []string{"id", "filename", "domain"},
		}, &documents)
		if err != nil {
			return nil, err
		}

		for _, document := range documents.Results {
			if documentString(document, "domain") == domain && documentString(document, "filename") == filename {
				id := documentString(document, "id")
				if id != "" {
					documentIDs = append(documentIDs, id)
				}
			}
		}

		if len(documents.Results) < deleteDocumentLookupLimit {
			break
		}
		offset += int64(len(documents.Results))
	}

	return documentIDs, nil
}

func deleteArchiveDocuments(index meilisearch.DocumentManager, documentIDs []string) (*meilisearch.TaskInfo, error) {
	if len(documentIDs) == 1 {
		return index.DeleteDocument(documentIDs[0])
	}
	return index.DeleteDocuments(documentIDs)
}

func documentString(document map[string]interface{}, key string) string {
	value, ok := document[key]
	if !ok || value == nil {
		return ""
	}
	stringValue, ok := value.(string)
	if ok {
		return stringValue
	}
	return fmt.Sprint(value)
}
