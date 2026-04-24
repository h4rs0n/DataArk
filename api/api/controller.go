package api

import (
	"DataArk/assets"
	"DataArk/common"
	"DataArk/search"
	"embed"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"html/template"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	tokenResponse, err := common.RegisterWithToken(req.Username, req.Password)
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
	tokenResponse, err := common.LoginWithToken(req.Username, req.Password)
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
	queryResult, pageAndHits := search.QueryByKeyword(queryString, pageNum)

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

	task, created, err := search.AddDocURLTask(archiveURL)
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

	task, err := search.GetArchiveTask(taskID)
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

	if err := search.AddDocFile(req.Files[0].Name, req.Domain); err != nil {
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
	common.InitDB()
	search.CreateDefaultIndex()
	if err := search.InitArchiveTaskQueue(); err != nil {
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

	err := router.Run("0.0.0.0:7845")
	if err != nil {
		fmt.Print("Maybe the port is already in use. Please check it.")
		return
	}
}
