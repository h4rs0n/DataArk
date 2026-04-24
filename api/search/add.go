package search

import (
	"DataArk/common"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
	"gorm.io/gorm"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ArchiveTaskStatusPending = "pending"
	ArchiveTaskStatusRunning = "running"
	ArchiveTaskStatusSuccess = "success"
	ArchiveTaskStatusFailed  = "failed"
)

const (
	archiveTaskQueueSize      = 64
	archiveTaskPollInterval   = 2 * time.Second
	archiveTaskPollTimeout    = 2 * time.Minute
	archiveFileDetectInterval = 500 * time.Millisecond
	archiveFileDetectTimeout  = 15 * time.Second
)

var (
	archiveTaskQueue     chan string
	archiveTaskQueueOnce sync.Once
	archiveTaskCreateMu  sync.Mutex
)

type singleFileTaskResponse struct {
	Message    string     `json:"message"`
	TaskID     string     `json:"taskId"`
	URL        string     `json:"url"`
	Status     string     `json:"status"`
	OutputDir  string     `json:"outputDir"`
	CreatedAt  *time.Time `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	FileName   string     `json:"fileName"`
	Error      string     `json:"error"`
}

func InitArchiveTaskQueue() error {
	var initErr error

	archiveTaskQueueOnce.Do(func() {
		tempDir := filepath.Join(common.ARCHIVEFILELOACTION, "Temporary")
		if err := os.MkdirAll(tempDir, os.ModePerm); err != nil {
			initErr = err
			return
		}

		archiveTaskQueue = make(chan string, archiveTaskQueueSize)
		go processArchiveTasks()

		// 队列本身只存在于进程内，所以启动时需要把数据库里未完成的任务重新塞回队列，
		// 否则服务重启后这些任务会永久停在 pending/running。
		pendingTasks, err := common.ListArchiveTasksByStatuses([]string{
			ArchiveTaskStatusPending,
			ArchiveTaskStatusRunning,
		})
		if err != nil {
			initErr = err
			return
		}

		for _, task := range pendingTasks {
			archiveTaskQueue <- task.ID
		}
	})

	return initErr
}

func AddDocFile(fileName string, originDomain string) (err error) {
	htmlFilePath := filepath.Join(common.ARCHIVEFILELOACTION, "Temporary", fileName)
	_, err = os.Stat(htmlFilePath)
	if err != nil {
		return err
	}
	return addDocFileByPath(htmlFilePath, fileName, originDomain)
}

func AddDocURLTask(rawURL string) (*common.ArchiveTask, bool, error) {
	if err := InitArchiveTaskQueue(); err != nil {
		return nil, false, err
	}

	normalizedURL, domain, err := normalizeArchiveURL(rawURL)
	if err != nil {
		return nil, false, err
	}

	archiveTaskCreateMu.Lock()
	defer archiveTaskCreateMu.Unlock()

	// 先查正在执行的任务，是为了保证同一个 URL 在外部 SingleFile 服务和我们内部索引链路里
	// 都只会有一个活跃任务，避免重复抓取、重复建索引。
	activeTask, err := common.FindActiveArchiveTaskByURL(normalizedURL)
	if err == nil {
		return activeTask, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	// 成功任务直接复用已有结果，而不是再次请求外部服务。
	// 这样做可以保持接口幂等，也避免同一页面被重复保存出多个归档文件。
	latestTask, err := common.GetLatestArchiveTaskByURL(normalizedURL)
	if err == nil && latestTask.Status == ArchiveTaskStatusSuccess {
		return latestTask, false, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	task := &common.ArchiveTask{
		ID:     uuid.New().String(),
		URL:    normalizedURL,
		Domain: domain,
		Status: ArchiveTaskStatusPending,
	}
	if err := common.CreateArchiveTask(task); err != nil {
		return nil, false, err
	}

	archiveTaskQueue <- task.ID
	return task, true, nil
}

func GetArchiveTask(taskID string) (*common.ArchiveTask, error) {
	if err := InitArchiveTaskQueue(); err != nil {
		return nil, err
	}
	return common.GetArchiveTaskByID(taskID)
}

func processArchiveTasks() {
	for taskID := range archiveTaskQueue {
		processArchiveTask(taskID)
	}
}

func processArchiveTask(taskID string) {
	task, err := common.GetArchiveTaskByID(taskID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("failed to load archive task %s: %v", taskID, err)
		}
		return
	}

	if task.Status == ArchiveTaskStatusSuccess {
		return
	}

	now := time.Now()
	task.Status = ArchiveTaskStatusRunning
	task.Error = ""
	task.FinishedAt = nil
	if task.StartedAt == nil {
		task.StartedAt = &now
	}
	if err := common.SaveArchiveTask(task); err != nil {
		log.Printf("failed to mark archive task %s as running: %v", task.ID, err)
		return
	}

	// 这里拆成“请求离线 -> 等待文件落盘 -> 复用现有索引逻辑”三步，
	// 是为了把外部服务的不确定性和本项目已有的 HTML 解析/索引逻辑解耦。
	singleFileResp, err := archiveURLToHTML(task.URL)
	if err != nil {
		finishArchiveTaskWithError(task, singleFileResp, err)
		return
	}

	if _, err := waitForArchivedFile(singleFileResp.FileName); err != nil {
		finishArchiveTaskWithError(task, singleFileResp, err)
		return
	}

	if err := addDownloadedDocFile(singleFileResp.FileName, task.Domain); err != nil {
		finishArchiveTaskWithError(task, singleFileResp, err)
		return
	}

	finishedAt := time.Now()
	task.Status = ArchiveTaskStatusSuccess
	task.Error = ""
	task.FileName = singleFileResp.FileName
	task.ExternalTaskID = singleFileResp.TaskID
	task.FinishedAt = &finishedAt
	if err := common.SaveArchiveTask(task); err != nil {
		log.Printf("failed to save successful archive task %s: %v", task.ID, err)
	}
}

func finishArchiveTaskWithError(task *common.ArchiveTask, resp *singleFileTaskResponse, err error) {
	finishedAt := time.Now()
	task.Status = ArchiveTaskStatusFailed
	task.Error = err.Error()
	task.FinishedAt = &finishedAt
	if resp != nil {
		task.ExternalTaskID = resp.TaskID
		if resp.FileName != "" {
			task.FileName = resp.FileName
		}
	}
	if saveErr := common.SaveArchiveTask(task); saveErr != nil {
		log.Printf("failed to save failed archive task %s: %v", task.ID, saveErr)
	}
}

func archiveURLToHTML(rawURL string) (*singleFileTaskResponse, error) {
	resp, err := createSingleFileTask(rawURL)
	if err != nil {
		return nil, err
	}
	if resp.Status == ArchiveTaskStatusSuccess {
		return resp, nil
	}
	if resp.Status == ArchiveTaskStatusFailed {
		return resp, buildSingleFileTaskError(resp)
	}

	// README 明确说明 /task/create 会按 URL 幂等返回当前任务状态，
	// 所以这里持续按同一个 URL 轮询即可，不需要自己再维护外部 taskId 到 URL 的映射。
	deadline := time.Now().Add(archiveTaskPollTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(archiveTaskPollInterval)

		resp, err = querySingleFileTask(rawURL)
		if err != nil {
			return resp, err
		}

		switch resp.Status {
		case ArchiveTaskStatusSuccess:
			return resp, nil
		case ArchiveTaskStatusFailed:
			return resp, buildSingleFileTaskError(resp)
		}
	}

	return resp, fmt.Errorf("等待 SingleFile WEBService 任务完成超时")
}

func buildSingleFileTaskError(resp *singleFileTaskResponse) error {
	if resp == nil {
		return fmt.Errorf("SingleFile WEBService 返回空响应")
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return fmt.Errorf("SingleFile WEBService 任务失败")
}

func createSingleFileTask(rawURL string) (*singleFileTaskResponse, error) {
	requestBody, err := json.Marshal(map[string]string{"url": rawURL})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, common.SINGLEFILEWEBSERVICEURL+"/task/create", bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	return executeSingleFileRequest(req)
}

func querySingleFileTask(rawURL string) (*singleFileTaskResponse, error) {
	queryURL := common.SINGLEFILEWEBSERVICEURL + "/task/create?url=" + neturl.QueryEscape(rawURL)
	req, err := http.NewRequest(http.MethodGet, queryURL, nil)
	if err != nil {
		return nil, err
	}

	return executeSingleFileRequest(req)
}

func executeSingleFileRequest(req *http.Request) (*singleFileTaskResponse, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("SingleFile WEBService 返回异常状态码 %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var taskResp singleFileTaskResponse
	if err := json.Unmarshal(respBody, &taskResp); err != nil {
		return nil, err
	}

	return &taskResp, nil
}

func waitForArchivedFile(fileName string) (string, error) {
	if fileName == "" {
		return "", fmt.Errorf("SingleFile WEBService 未返回文件名")
	}

	filePath := filepath.Join(common.ARCHIVEFILELOACTION, fileName)
	// 外部容器先返回 success，再通过共享卷把文件暴露给当前服务是有时间差的，
	// 所以这里额外等待文件真正出现在归档目录，避免后续解析阶段偶发找不到文件。
	deadline := time.Now().Add(archiveFileDetectTimeout)
	for time.Now().Before(deadline) {
		fileInfo, err := os.Stat(filePath)
		if err == nil && !fileInfo.IsDir() {
			return filePath, nil
		}
		time.Sleep(archiveFileDetectInterval)
	}

	return "", fmt.Errorf("未检测到离线 HTML 文件: %s", fileName)
}

func normalizeArchiveURL(rawURL string) (string, string, error) {
	normalizedURL := strings.TrimSpace(rawURL)
	if normalizedURL == "" {
		return "", "", fmt.Errorf("链接不能为空")
	}

	parsedURL, err := neturl.Parse(normalizedURL)
	if err != nil {
		return "", "", fmt.Errorf("链接格式错误")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", "", fmt.Errorf("仅支持 http 或 https 链接")
	}
	if parsedURL.Hostname() == "" {
		return "", "", fmt.Errorf("链接格式错误")
	}

	parsedURL.Fragment = ""
	return parsedURL.String(), strings.ToLower(parsedURL.Hostname()), nil
}

func addDownloadedDocFile(fileName string, originDomain string) error {
	htmlFilePath := filepath.Join(common.ARCHIVEFILELOACTION, fileName)
	_, err := os.Stat(htmlFilePath)
	if err != nil {
		return err
	}
	return addDocFileByPath(htmlFilePath, fileName, originDomain)
}

func addDocFileByPath(htmlFilePath string, fileName string, originDomain string) (err error) {
	HTMLContent, err := common.GetHTMLFileContent(htmlFilePath)
	if err != nil {
		return err
	}
	title, err := common.GetHTMLTitle(HTMLContent)
	if err != nil {
		return err
	}
	HTMLPureText, err := common.ExtractHTMLText(HTMLContent)
	if err != nil {
		return err
	}

	documents := []map[string]interface{}{
		{
			"id":       uuid.New().String(),
			"title":    title,
			"filename": fileName,
			"domain":   originDomain,
			"content":  HTMLPureText,
		},
	}
	client := meilisearch.New(common.MEILIHOST, meilisearch.WithAPIKey(common.MEILIAPIKey))

	_, err = client.Index(common.MEILIBlogsIndex).AddDocuments(documents)
	if err != nil {
		return err
	}

	// 成功添加索引内容后，移动文件到域名目录
	targetDir := filepath.Join(common.ARCHIVEFILELOACTION, originDomain)
	if err := os.MkdirAll(targetDir, os.ModePerm); err != nil {
		return err
	}
	err = os.Rename(htmlFilePath, filepath.Join(targetDir, fileName))
	if err != nil {
		return err
	}
	return nil
}

func CreateDefaultIndex() (err error) {
	client := meilisearch.New(common.MEILIHOST, meilisearch.WithAPIKey(common.MEILIAPIKey))
	_, err = client.GetIndex(common.MEILIBlogsIndex)
	if err == nil {
		return nil
	} else {
		client.CreateIndex(&meilisearch.IndexConfig{
			Uid:        common.MEILIBlogsIndex,
			PrimaryKey: "id",
		})
	}
	return nil
}
