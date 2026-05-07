package search

import (
	"DataArk/common"
	"context"
	"encoding/json"
	"errors"
	"github.com/meilisearch/meilisearch-go"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeArchiveURL(t *testing.T) {
	got, domain, err := normalizeArchiveURL(" https://Example.COM/path?q=1#fragment ")
	if err != nil {
		t.Fatalf("normalizeArchiveURL returned error: %v", err)
	}
	if got != "https://Example.COM/path?q=1" || domain != "example.com" {
		t.Fatalf("got url=%q domain=%q", got, domain)
	}

	for _, rawURL := range []string{"", "ftp://example.com", "https:///missing-host"} {
		if _, _, err := normalizeArchiveURL(rawURL); err == nil {
			t.Fatalf("normalizeArchiveURL(%q) should return error", rawURL)
		}
	}
}

func TestSingleFileRequestHelpers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") == "bad-status" {
			http.Error(w, "failed", http.StatusBadGateway)
			return
		}
		if r.URL.Query().Get("mode") == "bad-json" {
			_, _ = w.Write([]byte("{"))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/task/create" {
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["url"] == "" {
				t.Fatalf("unexpected create payload: %#v err=%v", payload, err)
			}
		}
		_ = json.NewEncoder(w).Encode(singleFileTaskResponse{
			TaskID:   "task-1",
			Status:   ArchiveTaskStatusSuccess,
			FileName: "page.html",
		})
	}))
	defer server.Close()

	oldSingleFileURL := common.SINGLEFILEWEBSERVICEURL
	t.Cleanup(func() {
		common.SINGLEFILEWEBSERVICEURL = oldSingleFileURL
	})
	common.SINGLEFILEWEBSERVICEURL = server.URL

	created, err := createSingleFileTask("https://example.com")
	if err != nil {
		t.Fatalf("createSingleFileTask returned error: %v", err)
	}
	if created.TaskID != "task-1" {
		t.Fatalf("created task = %#v", created)
	}
	queried, err := querySingleFileTask("https://example.com")
	if err != nil {
		t.Fatalf("querySingleFileTask returned error: %v", err)
	}
	if queried.FileName != "page.html" {
		t.Fatalf("queried task = %#v", queried)
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+"/task/create?mode=bad-status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executeSingleFileRequest(request); err == nil || !strings.Contains(err.Error(), "异常状态码") {
		t.Fatalf("bad status err = %v", err)
	}
	request, err = http.NewRequest(http.MethodGet, server.URL+"/task/create?mode=bad-json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executeSingleFileRequest(request); err == nil {
		t.Fatal("bad json should return error")
	}
}

func TestBuildSingleFileTaskError(t *testing.T) {
	if err := buildSingleFileTaskError(nil); err == nil || !strings.Contains(err.Error(), "空响应") {
		t.Fatalf("nil response err = %v", err)
	}
	if err := buildSingleFileTaskError(&singleFileTaskResponse{Error: "boom"}); err == nil || err.Error() != "boom" {
		t.Fatalf("explicit error = %v", err)
	}
	if err := buildSingleFileTaskError(&singleFileTaskResponse{}); err == nil || !strings.Contains(err.Error(), "任务失败") {
		t.Fatalf("fallback err = %v", err)
	}
}

func TestWaitForArchivedFile(t *testing.T) {
	oldArchiveRoot := common.ARCHIVEFILELOACTION
	t.Cleanup(func() {
		common.ARCHIVEFILELOACTION = oldArchiveRoot
	})
	root := t.TempDir()
	common.ARCHIVEFILELOACTION = root
	writeFile(t, filepath.Join(root, "page.html"), "<html></html>")

	got, err := waitForArchivedFile("page.html")
	if err != nil {
		t.Fatalf("waitForArchivedFile returned error: %v", err)
	}
	if got != filepath.Join(root, "page.html") {
		t.Fatalf("got %q, want page path", got)
	}
	if _, err := waitForArchivedFile(""); err == nil {
		t.Fatal("empty file name should return error")
	}
}

func TestMeiliArchiveIndexStoreListsDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/indexes/blogs/documents" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(meilisearch.DocumentsResult{
			Results: []map[string]interface{}{
				{"id": "1", "domain": "example.com", "filename": "one.html"},
				{"id": 2, "domain": "example.com", "filename": "two.html"},
			},
			Limit: 2,
		})
	}))
	defer server.Close()

	oldHost := common.MEILIHOST
	t.Cleanup(func() {
		common.MEILIHOST = oldHost
	})
	common.MEILIHOST = server.URL

	documents, err := (meiliArchiveIndexStore{}).ListArchiveDocuments(context.Background())
	if err != nil {
		t.Fatalf("ListArchiveDocuments returned error: %v", err)
	}
	if len(documents) != 2 || documents[1].ID != "2" {
		t.Fatalf("documents = %#v", documents)
	}
}

func TestRebuildRecoverableIndexFromArchiveSkipsInvalidHTML(t *testing.T) {
	root := t.TempDir()
	writeArchiveHTML(t, root, "example.com", "page.html", "Page", "body")
	writeFile(t, filepath.Join(root, "broken.example", "bad.html"), "<html><body>missing title</body></html>")

	var addedDocuments []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/indexes":
			_ = json.NewEncoder(w).Encode(meilisearch.IndexesResults{})
		case r.Method == http.MethodPost && r.URL.Path == "/indexes":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(meilisearch.TaskInfo{TaskUID: 1, Status: meilisearch.TaskStatusEnqueued})
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/1":
			_ = json.NewEncoder(w).Encode(meilisearch.Task{TaskUID: 1, Status: meilisearch.TaskStatusSucceeded})
		case r.Method == http.MethodPost && r.URL.Path == "/indexes/blogs/documents":
			if err := json.NewDecoder(r.Body).Decode(&addedDocuments); err != nil {
				t.Fatalf("failed to decode add documents payload: %v", err)
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(meilisearch.TaskInfo{TaskUID: 2, Status: meilisearch.TaskStatusEnqueued})
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/2":
			_ = json.NewEncoder(w).Encode(meilisearch.Task{TaskUID: 2, Status: meilisearch.TaskStatusSucceeded})
		default:
			t.Fatalf("unexpected meili request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	oldHost := common.MEILIHOST
	oldRoot := common.ARCHIVEFILELOACTION
	t.Cleanup(func() {
		common.MEILIHOST = oldHost
		common.ARCHIVEFILELOACTION = oldRoot
	})
	common.MEILIHOST = server.URL
	common.ARCHIVEFILELOACTION = root

	result, issues, err := RebuildRecoverableIndexFromArchive(context.Background())
	if err != nil {
		t.Fatalf("RebuildRecoverableIndexFromArchive returned error: %v", err)
	}
	if result.Documents != 1 || len(addedDocuments) != 1 {
		t.Fatalf("result=%#v added=%#v", result, addedDocuments)
	}
	if len(issues) != 1 || issues[0].Store != ArchiveConsistencyStoreHTML {
		t.Fatalf("issues = %#v, want one HTML parse issue", issues)
	}
}

func TestDeleteDocumentHelpersUseMeiliIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/indexes/blogs/documents":
			_ = json.NewEncoder(w).Encode(meilisearch.DocumentsResult{
				Results: []map[string]interface{}{
					{"id": "keep", "domain": "example.com", "filename": "other.html"},
					{"id": "delete-me", "domain": "example.com", "filename": "page.html"},
				},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/indexes/blogs/documents/delete-me":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(meilisearch.TaskInfo{TaskUID: 9})
		case r.Method == http.MethodPost && r.URL.Path == "/indexes/blogs/documents/delete-batch":
			var ids []string
			if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
				t.Fatal(err)
			}
			if strings.Join(ids, ",") != "a,b" {
				t.Fatalf("batch ids = %#v", ids)
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(meilisearch.TaskInfo{TaskUID: 10})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := meilisearch.New(server.URL)
	index := client.Index("blogs")
	ids, err := findArchiveDocumentIDs(index, "example.com", "page.html")
	if err != nil {
		t.Fatalf("findArchiveDocumentIDs returned error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "delete-me" {
		t.Fatalf("ids = %#v", ids)
	}
	task, err := deleteArchiveDocuments(index, ids)
	if err != nil {
		t.Fatalf("deleteArchiveDocuments single returned error: %v", err)
	}
	if task.TaskUID != 9 {
		t.Fatalf("single task = %#v", task)
	}
	task, err = deleteArchiveDocuments(index, []string{"a", "b"})
	if err != nil {
		t.Fatalf("deleteArchiveDocuments batch returned error: %v", err)
	}
	if task.TaskUID != 10 {
		t.Fatalf("batch task = %#v", task)
	}
}

func TestDocumentString(t *testing.T) {
	document := map[string]interface{}{"string": "value", "number": 3, "nil": nil}
	if documentString(document, "string") != "value" {
		t.Fatal("string value was not returned")
	}
	if documentString(document, "number") != "3" {
		t.Fatal("number value was not formatted")
	}
	if documentString(document, "nil") != "" || documentString(document, "missing") != "" {
		t.Fatal("nil or missing values should return empty string")
	}
}

func TestAddDocFileRejectsMissingTemporaryFile(t *testing.T) {
	oldRoot := common.ARCHIVEFILELOACTION
	t.Cleanup(func() {
		common.ARCHIVEFILELOACTION = oldRoot
	})
	common.ARCHIVEFILELOACTION = t.TempDir()

	err := AddDocFile("missing.html", "example.com")
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want not exist", err)
	}
}
