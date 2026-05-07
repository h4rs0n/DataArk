package api

import (
	"DataArk/assets"
	"DataArk/backup"
	"DataArk/common"
	"DataArk/search"
	"embed"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"html/template"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	checkArchiveConsistency  = search.CheckArchiveConsistency
	repairArchiveConsistency = search.RepairArchiveConsistency
	registerWithToken        = common.RegisterWithToken
	loginWithToken           = common.LoginWithToken
	queryByKeyword           = search.QueryByKeyword
	addDocURLTask            = search.AddDocURLTask
	getArchiveTask           = search.GetArchiveTask
	getArchiveStatsSnapshot  = common.GetArchiveStats
	refreshStatsFromDisk     = common.RefreshArchiveStatsFromDisk
	addDocFileToIndex        = search.AddDocFile
	deleteDocByHTMLPath      = search.DeleteDocByHTMLPath
	createBackupArchive      = backup.CreateBackup
	restoreBackupArchive     = backup.RestoreBackup
	initDatabase             = common.InitDB
	createSearchIndex        = search.CreateDefaultIndex
	initArchiveQueue         = search.InitArchiveTaskQueue
	runGinRouter             = func(router *gin.Engine, addr string) error {
		return router.Run(addr)
	}
)

// AuthController 认证控制器
type AuthController struct{}

// Register 用户注册
func (ac *AuthController) Register(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required,min=3,max=20"`
		Password string `json:"password" binding:"required,min=6"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"Status":  "0",
			"Message": "Invalid request data",
			"Error":   err.Error(),
		})
		return
	}

	// 注册用户并生成Token
	tokenResponse, err := registerWithToken(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"Status":  "0",
			"Error":   "Registration failed",
			"Message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"Status":  "1",
		"Message": "User registered successfully",
		"Data":    tokenResponse,
	})
}

// Login 用户登录
func (ac *AuthController) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"Status":  "0",
			"Message": "Invalid request data",
			"Error":   err.Error(),
		})
		return
	}

	// 登录并生成Token
	tokenResponse, err := loginWithToken(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"Status":  "0",
			"Error":   err.Error(),
			"Message": "Login failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"Status":  "1",
		"Message": "Login successful",
		"Data":    tokenResponse,
	})
}

func (ac *AuthController) AuthChecker(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"Status":  "1",
		"Message": "Already login",
	})
}

func SearchByKeyword(c *gin.Context) {
	queryString := c.Query("q")
	queryPage := c.Query("p")
	pageNum := int64(0)
	if queryPage == "" {
		pageNum = 1
	} else {
		var err error
		pageNum, err = strconv.ParseInt(queryPage, 10, 64)
		if err != nil {
			c.JSON(403, gin.H{
				"Status":  "0",
				"Message": "参数 p 格式错误",
			})
			return
		}
	}
	if queryString == "" {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "缺少关键参数 q",
		})
		return
	}
	queryResult, pageAndHits := queryByKeyword(queryString, pageNum)

	if queryResult == "Error" {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "查询失败",
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":     "1",
		"Message":    "",
		"Result":     queryResult,
		"TotalHits":  pageAndHits["TotalHits"],
		"TotalPages": pageAndHits["TotalPages"],
	})
}

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

func buildArchiveTaskResponse(task *common.ArchiveTask, created bool) (int, string) {
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

func AddHTMLFile(c *gin.Context) {
	htmlFile, err := c.FormFile("file")
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "上传文件失败",
		})
		return
	}

	tempDir := filepath.Join(common.ARCHIVEFILELOACTION, "Temporary")
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
	Domain string `json:"domain"`
	Files  []File `json:"files"`
}

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

	if err := addDocFileToIndex(req.Files[0].Name, req.Domain); err != nil {
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

func CreateBackup(c *gin.Context) {
	preparedBackup, err := createBackupArchive(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "创建备份失败",
			"Error":   err.Error(),
		})
		return
	}
	defer preparedBackup.Cleanup()

	reader, writer := io.Pipe()
	go func() {
		err := preparedBackup.WriteZip(writer)
		_ = writer.CloseWithError(err)
	}()

	c.DataFromReader(http.StatusOK, -1, "application/zip", reader, map[string]string{
		"Content-Disposition": fmt.Sprintf("attachment; filename=%q", preparedBackup.FileName),
		"Cache-Control":       "no-store",
	})
}

func RestoreBackup(c *gin.Context) {
	backupFile, err := c.FormFile("file")
	if err != nil {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "缺少备份文件",
		})
		return
	}
	if !strings.EqualFold(filepath.Ext(backupFile.Filename), ".zip") {
		c.JSON(403, gin.H{
			"Status":  "0",
			"Message": "备份文件必须是 zip 压缩包",
		})
		return
	}

	tempDir, err := os.MkdirTemp("", "dataark-restore-upload-*")
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "初始化恢复临时目录失败",
			"Error":   err.Error(),
		})
		return
	}
	defer os.RemoveAll(tempDir)

	zipPath := filepath.Join(tempDir, "backup.zip")
	if err := c.SaveUploadedFile(backupFile, zipPath); err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "保存备份文件失败",
			"Error":   err.Error(),
		})
		return
	}

	result, err := restoreBackupArchive(c.Request.Context(), zipPath)
	if err != nil {
		c.JSON(500, gin.H{
			"Status":  "0",
			"Message": "恢复备份失败",
			"Error":   err.Error(),
		})
		return
	}

	c.JSON(200, gin.H{
		"Status":  "1",
		"Message": "备份恢复成功",
		"Data":    result,
	})
}

var Templates embed.FS

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func WebStarter(debugMode bool) {
	if !debugMode {
		gin.SetMode(gin.ReleaseMode)
	}
	initDatabase()
	createSearchIndex()
	if err := initArchiveQueue(); err != nil {
		fmt.Printf("failed to initialize archive task queue: %v\n", err)
		return
	}
	router := gin.Default()
	if debugMode {
		router.Use(CORSMiddleware())
	}
	authController := &AuthController{}
	public := router.Group("/api")
	{
		public.POST("/login", authController.Login)
	}
	protected := router.Group("/api")
	protected.Use(AuthMiddleware())
	{
		protected.GET("/search", SearchByKeyword)
		protected.POST("/uploadHtmlFile", AddHTMLFile)
		protected.POST("/upload", AddDocByHTMLFile)
		protected.POST("/archiveByURL", AddDocByURL)
		protected.GET("/archiveTask/:taskId", GetArchiveTaskStatus)
		protected.GET("/archiveStats", GetArchiveStats)
		protected.POST("/archiveStats/refresh", RefreshArchiveStats)
		protected.GET("/archiveConsistency", GetArchiveConsistency)
		protected.POST("/archiveConsistency/repair", RepairArchiveConsistency)
		protected.DELETE("/archive", DeleteArchiveDocument)
		protected.POST("/backup", CreateBackup)
		protected.POST("/backup/restore", RestoreBackup)
		protected.GET("/authChecker", authController.AuthChecker)
		protected.POST("/register", authController.Register)
	}
	archiveGroup := router.Group("/")
	archiveGroup.Use(AuthMiddleware())
	{
		archiveGroup.Static("/archive", common.ARCHIVEFILELOACTION)
	}
	router.Static("/static", "./static/web/")
	router.StaticFS("/assets", http.FS(assets.LoadFile()))

	tmpl := template.Must(template.New("").ParseFS(assets.WebFiles, "web/*.html"))
	router.SetHTMLTemplate(tmpl)

	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})

	err := runGinRouter(router, "0.0.0.0:7845")
	if err != nil {
		fmt.Print("Maybe the port is already in use. Please check it.")
		return
	}
}
