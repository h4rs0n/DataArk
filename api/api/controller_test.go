package api

import (
	"DataArk/backup"
	"DataArk/common"
	"DataArk/search"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestGetArchiveConsistencyReturnsReport(t *testing.T) {
	oldCheck := checkArchiveConsistency
	t.Cleanup(func() {
		checkArchiveConsistency = oldCheck
	})
	checkArchiveConsistency = func(context.Context) (*search.ArchiveConsistencyReport, error) {
		return &search.ArchiveConsistencyReport{Consistent: true, HTMLFiles: 2}, nil
	}

	response := performControllerRequest(http.MethodGet, "/consistency", GetArchiveConsistency)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	payload := decodeResponse(t, response)
	if payload["Status"] != "1" {
		t.Fatalf("payload Status = %#v, want 1", payload["Status"])
	}
	data := payload["Data"].(map[string]interface{})
	if data["htmlFiles"] != float64(2) {
		t.Fatalf("htmlFiles = %#v, want 2", data["htmlFiles"])
	}
}

func TestGetArchiveConsistencyReturnsError(t *testing.T) {
	oldCheck := checkArchiveConsistency
	t.Cleanup(func() {
		checkArchiveConsistency = oldCheck
	})
	checkArchiveConsistency = func(context.Context) (*search.ArchiveConsistencyReport, error) {
		return nil, errors.New("meili unavailable")
	}

	response := performControllerRequest(http.MethodGet, "/consistency", GetArchiveConsistency)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	payload := decodeResponse(t, response)
	if payload["Status"] != "0" || payload["Error"] != "meili unavailable" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestRepairArchiveConsistencyReturnsReport(t *testing.T) {
	oldRepair := repairArchiveConsistency
	t.Cleanup(func() {
		repairArchiveConsistency = oldRepair
	})
	repairArchiveConsistency = func(context.Context) (*search.ArchiveConsistencyReport, error) {
		return &search.ArchiveConsistencyReport{IndexedDocuments: 3}, nil
	}

	response := performControllerRequest(http.MethodPost, "/consistency/repair", RepairArchiveConsistency)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	payload := decodeResponse(t, response)
	data := payload["Data"].(map[string]interface{})
	if data["indexedDocuments"] != float64(3) {
		t.Fatalf("indexedDocuments = %#v, want 3", data["indexedDocuments"])
	}
}

func TestRepairArchiveConsistencyReturnsError(t *testing.T) {
	oldRepair := repairArchiveConsistency
	t.Cleanup(func() {
		repairArchiveConsistency = oldRepair
	})
	repairArchiveConsistency = func(context.Context) (*search.ArchiveConsistencyReport, error) {
		return nil, errors.New("rebuild failed")
	}

	response := performControllerRequest(http.MethodPost, "/consistency/repair", RepairArchiveConsistency)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	payload := decodeResponse(t, response)
	if payload["Error"] != "rebuild failed" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestBuildArchiveTaskResponse(t *testing.T) {
	cases := []struct {
		name       string
		task       *common.ArchiveTask
		created    bool
		wantStatus int
		wantText   string
	}{
		{name: "created", task: &common.ArchiveTask{Status: search.ArchiveTaskStatusPending}, created: true, wantStatus: http.StatusAccepted, wantText: "已加入队列"},
		{name: "running", task: &common.ArchiveTask{Status: search.ArchiveTaskStatusRunning}, wantStatus: http.StatusAccepted, wantText: "正在处理中"},
		{name: "success", task: &common.ArchiveTask{Status: search.ArchiveTaskStatusSuccess}, wantStatus: http.StatusOK, wantText: "已完成"},
		{name: "failed", task: &common.ArchiveTask{Status: search.ArchiveTaskStatusFailed}, wantStatus: http.StatusOK, wantText: "执行失败"},
		{name: "unknown", task: &common.ArchiveTask{Status: "paused"}, wantStatus: http.StatusOK, wantText: "状态已返回"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, message := buildArchiveTaskResponse(tc.task, tc.created)
			if status != tc.wantStatus || !contains(message, tc.wantText) {
				t.Fatalf("got status=%d message=%q, want status=%d contains %q", status, message, tc.wantStatus, tc.wantText)
			}
		})
	}
}

func TestAuthControllerRegisterAndLogin(t *testing.T) {
	controller := &AuthController{}
	oldRegister := registerWithToken
	oldLogin := loginWithToken
	t.Cleanup(func() {
		registerWithToken = oldRegister
		loginWithToken = oldLogin
	})

	registerWithToken = func(username string, password string) (*common.TokenResponse, error) {
		if username != "alice" || password != "secret1" {
			t.Fatalf("unexpected register input %q %q", username, password)
		}
		return &common.TokenResponse{Token: "registered"}, nil
	}
	response := performJSONControllerRequest(http.MethodPost, "/register", `{"username":"alice","password":"secret1"}`, controller.Register)
	if response.Code != http.StatusCreated {
		t.Fatalf("register status = %d, want 201", response.Code)
	}

	registerWithToken = func(string, string) (*common.TokenResponse, error) {
		return nil, errors.New("duplicate")
	}
	response = performJSONControllerRequest(http.MethodPost, "/register", `{"username":"alice","password":"secret1"}`, controller.Register)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("register failure status = %d, want 400", response.Code)
	}
	response = performJSONControllerRequest(http.MethodPost, "/register", `{`, controller.Register)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid register status = %d, want 400", response.Code)
	}

	loginWithToken = func(username string, password string) (*common.TokenResponse, error) {
		return &common.TokenResponse{Token: username + ":" + password}, nil
	}
	response = performJSONControllerRequest(http.MethodPost, "/login", `{"username":"alice","password":"secret1"}`, controller.Login)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", response.Code)
	}

	loginWithToken = func(string, string) (*common.TokenResponse, error) {
		return nil, errors.New("invalid")
	}
	response = performJSONControllerRequest(http.MethodPost, "/login", `{"username":"alice","password":"secret1"}`, controller.Login)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("login failure status = %d, want 401", response.Code)
	}
	response = performJSONControllerRequest(http.MethodPost, "/login", `{`, controller.Login)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid login status = %d, want 400", response.Code)
	}
}

func TestSearchByKeywordBranches(t *testing.T) {
	oldQuery := queryByKeyword
	t.Cleanup(func() {
		queryByKeyword = oldQuery
	})

	response := performControllerRequest(http.MethodGet, "/search", SearchByKeyword)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing q status = %d, want 403", response.Code)
	}
	response = performControllerRequest(http.MethodGet, "/search?q=test&p=bad", SearchByKeyword)
	if response.Code != http.StatusForbidden {
		t.Fatalf("bad page status = %d, want 403", response.Code)
	}

	queryByKeyword = func(keyword string, pageNum int64) (string, map[string]int) {
		if keyword != "test" || pageNum != 2 {
			t.Fatalf("unexpected query %q page %d", keyword, pageNum)
		}
		return `[{"title":"hit"}]`, map[string]int{"TotalHits": 1, "TotalPages": 1}
	}
	response = performControllerRequest(http.MethodGet, "/search?q=test&p=2", SearchByKeyword)
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200", response.Code)
	}

	queryByKeyword = func(string, int64) (string, map[string]int) {
		return "Error", nil
	}
	response = performControllerRequest(http.MethodGet, "/search?q=test", SearchByKeyword)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("search error status = %d, want 500", response.Code)
	}
}

func TestAddDocByURLBranches(t *testing.T) {
	oldAdd := addDocURLTask
	t.Cleanup(func() {
		addDocURLTask = oldAdd
	})

	response := performJSONControllerRequest(http.MethodPost, "/archiveByURL", `{`, AddDocByURL)
	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid json status = %d, want 403", response.Code)
	}
	response = performJSONControllerRequest(http.MethodPost, "/archiveByURL", `{"url":"ftp://example.com"}`, AddDocByURL)
	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid url status = %d, want 403", response.Code)
	}

	addDocURLTask = func(rawURL string) (*common.ArchiveTask, bool, error) {
		if rawURL != "https://example.com" {
			t.Fatalf("rawURL = %q", rawURL)
		}
		return &common.ArchiveTask{ID: "task", Status: search.ArchiveTaskStatusPending}, true, nil
	}
	response = performJSONControllerRequest(http.MethodPost, "/archiveByURL", `{"url":"https://example.com"}`, AddDocByURL)
	if response.Code != http.StatusAccepted {
		t.Fatalf("success status = %d, want 202", response.Code)
	}

	addDocURLTask = func(string) (*common.ArchiveTask, bool, error) {
		return nil, false, errors.New("queue down")
	}
	response = performJSONControllerRequest(http.MethodPost, "/archiveByURL", `{"url":"https://example.com"}`, AddDocByURL)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("error status = %d, want 500", response.Code)
	}
}

func TestArchiveTaskAndStatsHandlers(t *testing.T) {
	oldTask := getArchiveTask
	oldStats := getArchiveStatsSnapshot
	oldRefresh := refreshStatsFromDisk
	t.Cleanup(func() {
		getArchiveTask = oldTask
		getArchiveStatsSnapshot = oldStats
		refreshStatsFromDisk = oldRefresh
	})

	getArchiveTask = func(id string) (*common.ArchiveTask, error) {
		return &common.ArchiveTask{ID: id, Status: search.ArchiveTaskStatusSuccess}, nil
	}
	response := performPathControllerRequest(http.MethodGet, "/archiveTask/:taskId", "/archiveTask/task-1", GetArchiveTaskStatus)
	if response.Code != http.StatusOK {
		t.Fatalf("task status = %d, want 200", response.Code)
	}
	getArchiveTask = func(string) (*common.ArchiveTask, error) {
		return nil, gorm.ErrRecordNotFound
	}
	response = performPathControllerRequest(http.MethodGet, "/archiveTask/:taskId", "/archiveTask/missing", GetArchiveTaskStatus)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing task status = %d, want 404", response.Code)
	}
	getArchiveTask = func(string) (*common.ArchiveTask, error) {
		return nil, errors.New("db down")
	}
	response = performPathControllerRequest(http.MethodGet, "/archiveTask/:taskId", "/archiveTask/error", GetArchiveTaskStatus)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("task error status = %d, want 500", response.Code)
	}

	getArchiveStatsSnapshot = func() (*common.ArchiveStatsSnapshot, error) {
		return &common.ArchiveStatsSnapshot{TotalFiles: 2}, nil
	}
	response = performControllerRequest(http.MethodGet, "/stats", GetArchiveStats)
	if response.Code != http.StatusOK {
		t.Fatalf("stats status = %d, want 200", response.Code)
	}
	getArchiveStatsSnapshot = func() (*common.ArchiveStatsSnapshot, error) {
		return nil, errors.New("stats failed")
	}
	response = performControllerRequest(http.MethodGet, "/stats", GetArchiveStats)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("stats error status = %d, want 500", response.Code)
	}

	refreshStatsFromDisk = func() (*common.ArchiveStatsSnapshot, error) {
		return &common.ArchiveStatsSnapshot{TotalFiles: 3}, nil
	}
	response = performControllerRequest(http.MethodPost, "/stats/refresh", RefreshArchiveStats)
	if response.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200", response.Code)
	}
	refreshStatsFromDisk = func() (*common.ArchiveStatsSnapshot, error) {
		return nil, errors.New("scan failed")
	}
	response = performControllerRequest(http.MethodPost, "/stats/refresh", RefreshArchiveStats)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("refresh error status = %d, want 500", response.Code)
	}
}

func TestAddDocByHTMLFileBranches(t *testing.T) {
	oldAdd := addDocFileToIndex
	t.Cleanup(func() {
		addDocFileToIndex = oldAdd
	})

	response := performJSONControllerRequest(http.MethodPost, "/upload", `{`, AddDocByHTMLFile)
	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid json status = %d, want 403", response.Code)
	}
	response = performJSONControllerRequest(http.MethodPost, "/upload", `{"domain":"","files":[]}`, AddDocByHTMLFile)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing domain status = %d, want 403", response.Code)
	}
	response = performJSONControllerRequest(http.MethodPost, "/upload", `{"domain":"example.com","files":[]}`, AddDocByHTMLFile)
	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong file count status = %d, want 403", response.Code)
	}
	response = performJSONControllerRequest(http.MethodPost, "/upload", `{"domain":"example.com","files":[{"name":""}]}`, AddDocByHTMLFile)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing file name status = %d, want 403", response.Code)
	}

	addDocFileToIndex = func(fileName string, originDomain string) error {
		if fileName != "page.html" || originDomain != "example.com" {
			t.Fatalf("unexpected add doc input %q %q", fileName, originDomain)
		}
		return nil
	}
	response = performJSONControllerRequest(http.MethodPost, "/upload", `{"domain":"example.com","files":[{"name":"page.html"}]}`, AddDocByHTMLFile)
	if response.Code != http.StatusOK {
		t.Fatalf("success status = %d, want 200", response.Code)
	}
	addDocFileToIndex = func(string, string) error { return errors.New("index failed") }
	response = performJSONControllerRequest(http.MethodPost, "/upload", `{"domain":"example.com","files":[{"name":"page.html"}]}`, AddDocByHTMLFile)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("error status = %d, want 500", response.Code)
	}
}

func TestDeleteArchiveDocumentBranches(t *testing.T) {
	oldDelete := deleteDocByHTMLPath
	t.Cleanup(func() {
		deleteDocByHTMLPath = oldDelete
	})

	response := performControllerRequest(http.MethodDelete, "/archive", DeleteArchiveDocument)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing path status = %d, want 403", response.Code)
	}

	deleteDocByHTMLPath = func(context.Context, string) (*search.DeleteDocResult, error) {
		return nil, search.ErrInvalidArchivePath
	}
	response = performControllerRequest(http.MethodDelete, "/archive?path=/bad", DeleteArchiveDocument)
	if response.Code != http.StatusForbidden {
		t.Fatalf("invalid path status = %d, want 403", response.Code)
	}
	deleteDocByHTMLPath = func(context.Context, string) (*search.DeleteDocResult, error) {
		return nil, search.ErrArchiveDocumentNotFound
	}
	response = performJSONControllerRequest(http.MethodDelete, "/archive", `{"path":"/archive/example/page.html"}`, DeleteArchiveDocument)
	if response.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want 404", response.Code)
	}
	deleteDocByHTMLPath = func(context.Context, string) (*search.DeleteDocResult, error) {
		return nil, errors.New("delete failed")
	}
	response = performControllerRequest(http.MethodDelete, "/archive?path=/archive/example/page.html", DeleteArchiveDocument)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("error status = %d, want 500", response.Code)
	}
	deleteDocByHTMLPath = func(context.Context, string) (*search.DeleteDocResult, error) {
		return &search.DeleteDocResult{Path: "/archive/example/page.html"}, nil
	}
	response = performControllerRequest(http.MethodDelete, "/archive?path=/archive/example/page.html", DeleteArchiveDocument)
	if response.Code != http.StatusOK {
		t.Fatalf("success status = %d, want 200", response.Code)
	}
}

func TestAddHTMLFile(t *testing.T) {
	oldRoot := common.ARCHIVEFILELOACTION
	t.Cleanup(func() {
		common.ARCHIVEFILELOACTION = oldRoot
	})
	common.ARCHIVEFILELOACTION = t.TempDir()

	response := performControllerRequest(http.MethodPost, "/uploadHtmlFile", AddHTMLFile)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing file status = %d, want 500", response.Code)
	}

	body, contentType := multipartBody(t, "file", "page.html", "<html></html>")
	response = performRawControllerRequest(http.MethodPost, "/uploadHtmlFile", body, contentType, AddHTMLFile)
	if response.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want 200 body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(common.ARCHIVEFILELOACTION, "Temporary", "page.html")); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
}

func TestBackupHandlers(t *testing.T) {
	oldCreate := createBackupArchive
	oldRestore := restoreBackupArchive
	t.Cleanup(func() {
		createBackupArchive = oldCreate
		restoreBackupArchive = oldRestore
	})

	createBackupArchive = func(context.Context) (*backup.PreparedBackup, error) {
		return nil, errors.New("backup failed")
	}
	response := performControllerRequest(http.MethodPost, "/backup", CreateBackup)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("backup error status = %d, want 500", response.Code)
	}

	root := t.TempDir()
	backupDir := filepath.Join(root, "backup")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	createBackupArchive = func(context.Context) (*backup.PreparedBackup, error) {
		return &backup.PreparedBackup{RootDir: root, BackupDir: backupDir, FileName: "backup.zip"}, nil
	}
	response = performControllerRequest(http.MethodPost, "/backup", CreateBackup)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Disposition"), "backup.zip") {
		t.Fatalf("backup success status=%d headers=%v", response.Code, response.Header())
	}

	response = performControllerRequest(http.MethodPost, "/backup/restore", RestoreBackup)
	if response.Code != http.StatusForbidden {
		t.Fatalf("restore missing file status = %d, want 403", response.Code)
	}
	body, contentType := multipartBody(t, "file", "backup.txt", "bad")
	response = performRawControllerRequest(http.MethodPost, "/backup/restore", body, contentType, RestoreBackup)
	if response.Code != http.StatusForbidden {
		t.Fatalf("restore wrong extension status = %d, want 403", response.Code)
	}
	restoreBackupArchive = func(context.Context, string) (*backup.RestoreResult, error) {
		return &backup.RestoreResult{IndexedDocuments: 4}, nil
	}
	body, contentType = multipartBody(t, "file", "backup.zip", "zip content")
	response = performRawControllerRequest(http.MethodPost, "/backup/restore", body, contentType, RestoreBackup)
	if response.Code != http.StatusOK {
		t.Fatalf("restore success status = %d, want 200 body=%s", response.Code, response.Body.String())
	}
	restoreBackupArchive = func(context.Context, string) (*backup.RestoreResult, error) {
		return nil, errors.New("restore failed")
	}
	body, contentType = multipartBody(t, "file", "backup.zip", "zip content")
	response = performRawControllerRequest(http.MethodPost, "/backup/restore", body, contentType, RestoreBackup)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("restore error status = %d, want 500", response.Code)
	}
}

func TestCORSMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORSMiddleware())
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodOptions, "/", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("options status = %d, want 204", response.Code)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Header().Get("Access-Control-Allow-Origin") != "*" || response.Body.String() != "ok" {
		t.Fatalf("unexpected cors response headers=%v body=%q", response.Header(), response.Body.String())
	}
}

func TestWebStarterInitializesAndRunsRouter(t *testing.T) {
	oldInitDB := initDatabase
	oldCreateIndex := createSearchIndex
	oldInitQueue := initArchiveQueue
	oldRun := runGinRouter
	t.Cleanup(func() {
		initDatabase = oldInitDB
		createSearchIndex = oldCreateIndex
		initArchiveQueue = oldInitQueue
		runGinRouter = oldRun
	})

	var calls []string
	initDatabase = func() { calls = append(calls, "db") }
	createSearchIndex = func() error {
		calls = append(calls, "index")
		return nil
	}
	initArchiveQueue = func() error {
		calls = append(calls, "queue")
		return nil
	}
	runGinRouter = func(router *gin.Engine, addr string) error {
		calls = append(calls, "run:"+addr)
		if len(router.Routes()) == 0 {
			t.Fatal("router should have registered routes")
		}
		return errors.New("port used")
	}

	WebStarter(false)

	if strings.Join(calls, ",") != "db,index,queue,run:0.0.0.0:7845" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestWebStarterStopsWhenQueueInitializationFails(t *testing.T) {
	oldInitDB := initDatabase
	oldCreateIndex := createSearchIndex
	oldInitQueue := initArchiveQueue
	oldRun := runGinRouter
	t.Cleanup(func() {
		initDatabase = oldInitDB
		createSearchIndex = oldCreateIndex
		initArchiveQueue = oldInitQueue
		runGinRouter = oldRun
	})

	initDatabase = func() {}
	createSearchIndex = func() error { return nil }
	initArchiveQueue = func() error { return errors.New("queue failed") }
	runGinRouter = func(*gin.Engine, string) error {
		t.Fatal("router should not run when queue initialization fails")
		return nil
	}

	WebStarter(true)
}

func performControllerRequest(method string, target string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return performRawControllerRequest(method, target, nil, "", handler)
}

func performJSONControllerRequest(method string, target string, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	return performRawControllerRequest(method, target, strings.NewReader(body), "application/json", handler)
}

func performPathControllerRequest(method string, routePath string, target string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Handle(method, routePath, handler)
	request := httptest.NewRequest(method, target, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func performRawControllerRequest(method string, target string, body io.Reader, contentType string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routePath := strings.Split(target, "?")[0]
	router.Handle(method, routePath, handler)
	request := httptest.NewRequest(method, target, body)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return payload
}

func contains(value string, part string) bool {
	return strings.Contains(value, part)
}

func multipartBody(t *testing.T, fieldName string, fileName string, content string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}
