package api

import (
	"DataArk/archive"
	"DataArk/config"
	"DataArk/search"
	"errors"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// archive.go 提供上传、URL 归档、统计、一致性与排行 HTTP 接口。

// AddDocByURL 根据 URL 创建离线归档任务。
func AddDocByURL(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "请求参数错误",
		})
		return
	}

	archiveURL := strings.TrimSpace(req.URL)
	parsedURL, err := neturl.Parse(archiveURL)
	if archiveURL == "" || err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "链接格式错误",
		})
		return
	}

	task, created, err := addDocURLTask(archiveURL)
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "创建离线任务失败",
			"Error":   err.Error(),
		})
		return
	}

	statusCode, message := buildArchiveTaskResponse(task, created)
	c.JSON(statusCode, gin.H{
		"Status":  "1",
		"Message": message,
		"Data":    task,
	})
}

// GetArchiveTaskStatus 查询离线归档任务状态。
func GetArchiveTaskStatus(c *gin.Context) {
	taskID := c.Param("taskId")
	if strings.TrimSpace(taskID) == "" {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "缺少任务编号",
		})
		return
	}

	task, err := getArchiveTask(taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{
				"Status":  "0",
				"Message": "任务不存在",
			})
			return
		}

		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "查询离线任务失败",
			"Error":   err.Error(),
		})
		return
	}

	statusCode, message := buildArchiveTaskResponse(task, false)
	c.JSON(statusCode, gin.H{
		"Status":  "1",
		"Message": message,
		"Data":    task,
	})
}

// GetArchiveStats 返回已入库的归档统计快照，不触发磁盘扫描。
func GetArchiveStats(c *gin.Context) {
	stats, err := getArchiveStatsSnapshot()
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "查询统计信息失败",
			"Error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "查询统计信息成功",
		"Data":    stats,
	})
}

// RefreshArchiveStats 扫描归档目录并用扫描结果重建统计表。
func RefreshArchiveStats(c *gin.Context) {
	stats, err := refreshStatsFromDisk()
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "刷新统计信息失败",
			"Error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "刷新统计信息成功",
		"Data":    stats,
	})
}

// RecordArchiveClick 记录归档文档点击。
func RecordArchiveClick(c *gin.Context) {
	var req struct {
		Path    string `json:"path"`
		Keyword string `json:"keyword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		c.JSON(403, gin.H{"Status": "0", "Message": "请求参数错误"})
		return
	}
	event, err := recordArchiveClick(req.Path, req.Keyword)
	if err != nil {
		c.JSON(403, gin.H{"Status": "0", "Message": "归档路径参数错误", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "点击记录成功", "Data": event})
}

// GetArchiveRankings 返回归档点击排行。
func GetArchiveRankings(c *gin.Context) {
	rankings, err := getArchiveRankings(c.DefaultQuery("window", "7d"), queryInt(c, "limit", 20))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询点击排行失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询点击排行成功", "Data": rankings})
}

// GetArchiveRecommendations 返回基于归档的推荐列表。
func GetArchiveRecommendations(c *gin.Context) {
	recommendations, err := getArchiveRecommendations(c.DefaultQuery("window", "7d"), queryInt(c, "limit", 20))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询归档推荐失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询归档推荐成功", "Data": recommendations})
}

// GetArchiveConsistency 检查归档文件与索引一致性。
func GetArchiveConsistency(c *gin.Context) {
	report, err := checkArchiveConsistency(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "检查归档一致性失败",
			"Error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "检查归档一致性成功",
		"Data":    report,
	})
}

// RepairArchiveConsistency 修复归档文件与索引不一致项。
func RepairArchiveConsistency(c *gin.Context) {
	report, err := repairArchiveConsistency(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "修复归档一致性失败",
			"Error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "修复归档一致性完成",
		"Data":    report,
	})
}

func buildArchiveTaskResponse(task *archive.ArchiveTask, created bool) (int, string) {
	if created {
		return http.StatusAccepted, "链接离线任务已加入队列"
	}

	// pending/running 都返回 202，是为了明确告诉前端这不是同步完成型接口，
	// 调用方应该继续轮询任务状态，而不是把这次响应误判成最终结果。
	switch task.Status {
	case search.ArchiveTaskStatusPending, search.ArchiveTaskStatusRunning:
		return http.StatusAccepted, "链接离线任务正在处理中"
	case search.ArchiveTaskStatusSuccess:
		return http.StatusOK, "链接离线任务已完成"
	case search.ArchiveTaskStatusFailed:
		return http.StatusOK, "链接离线任务执行失败"
	default:
		return http.StatusOK, "链接离线任务状态已返回"
	}
}

// AddHTMLFile 将 HTML 文件上传到临时目录。
func AddHTMLFile(c *gin.Context) {
	htmlFile, err := c.FormFile("file")
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "上传文件失败",
		})
		return
	}

	tempDir := filepath.Join(config.ARCHIVEFILELOACTION, "Temporary")
	if err := os.MkdirAll(tempDir, os.ModePerm); err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "初始化临时目录失败",
		})
		return
	}
	// 上传到临时目录
	filePath := filepath.Join(tempDir, htmlFile.Filename)

	if err := c.SaveUploadedFile(htmlFile, filePath); err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "上传文件失败",
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "文件上传成功",
	})
}

type File struct {
	Uid      string   `json:"uid"`
	File     struct{} `json:"file"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Percent  int      `json:"percent"`
	Response struct {
		Message string `json:"Message"`
		Status  string `json:"Status"`
	} `json:"response"`
}

type AddDocRequest struct {
	Domain    string `json:"domain"`
	SourceURL string `json:"sourceUrl"`
	Files     []File `json:"files"`
}

// AddDocByHTMLFile 将已上传的 HTML 文件入库并建立索引。
func AddDocByHTMLFile(c *gin.Context) {
	var req AddDocRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "请求参数错误",
		})
		return
	}
	if req.Domain == "" {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "请求参数错误",
		})
		return
	}
	if len(req.Files) != 1 {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "仅支持单个文件上传",
		})
		return
	}
	if req.Files[0].Name == "" {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "请求参数错误",
		})
		return
	}

	sourceURL := strings.TrimSpace(req.SourceURL)
	if sourceURL != "" {
		parsedURL, err := neturl.Parse(sourceURL)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" {
			c.JSON(403, gin.H{
				"Status":  "0",
				"Message": "原文链接格式错误",
			})
			return
		}
		parsedURL.Fragment = ""
		sourceURL = parsedURL.String()
	}

	if err := addDocFileToIndex(req.Files[0].Name, req.Domain, sourceURL); err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "上传文件失败",
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "文件上传成功",
	})
	return
}

// DeleteArchiveDocument 按路径删除归档文档。
func DeleteArchiveDocument(c *gin.Context) {
	htmlPath := strings.TrimSpace(c.Query("path"))
	if htmlPath == "" {
		var req struct {
			Path string `json:"path"`
		}
		if err := c.ShouldBindJSON(&req); err == nil {
			htmlPath = strings.TrimSpace(req.Path)
		}
	}

	if htmlPath == "" {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "缺少关键参数 path",
		})
		return
	}

	result, err := deleteDocByHTMLPath(c.Request.Context(), htmlPath)
	if err != nil {
		switch {
		case errors.Is(err, search.ErrInvalidArchivePath):
			c.JSON(403, gin.H{
				"Status":  "0",
				"Message": "HTML 路径参数错误",
				"Error":   err.Error(),
			})
		case errors.Is(err, search.ErrArchiveDocumentNotFound), errors.Is(err, search.ErrArchiveFileNotFound):
			c.JSON(404, gin.H{
				"Status":  "0",
				"Message": "文档不存在",
				"Error":   err.Error(),
			})
		default:
			c.JSON(500, gin.H{
				"Status":  "0",
				"Message": "删除文档失败",
				"Error":   err.Error(),
			})
		}
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "文档删除成功",
		"Data":    result,
	})
}
