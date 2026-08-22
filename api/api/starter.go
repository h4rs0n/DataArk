package api

import (
	"DataArk/assets"
	"DataArk/config"
	"context"
	"embed"
	"html/template"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// starter.go 负责进程启动：数据库、队列、调度器、Gin 与静态资源。

var Templates embed.FS

// CORSMiddleware 在 debug 模式下放开跨域请求。
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

// WebStarter 初始化依赖并启动 HTTP 服务，监听 0.0.0.0:7845。
func WebStarter(debugMode bool) {
	if !debugMode {
		gin.SetMode(gin.ReleaseMode)
	}
	initDatabase()
	createSearchIndex()
	if err := initArchiveQueue(); err != nil {
		log.Printf("failed to initialize archive task queue: %v", err)
		return
	}
	stopSharedJobQueue, err := startSharedJobQueue(context.Background())
	if err != nil {
		log.Printf("failed to initialize shared job queue: %v", err)
		stopSharedJobQueue = func() {}
	}
	defer stopSharedJobQueue()
	stopDiscoveryScheduler := startDiscoveryScheduler()
	defer stopDiscoveryScheduler()
	stopRecommendationScheduler := startRecommendationScheduler()
	defer stopRecommendationScheduler()
	router := gin.Default()
	if debugMode {
		router.Use(CORSMiddleware())
	}
	authController := &AuthController{}
	registerAPIRoutes(router, authController)
	archiveGroup := router.Group("/")
	archiveGroup.Use(AuthMiddleware())
	{
		archiveGroup.Static("/archive", config.ARCHIVEFILELOACTION)
	}
	router.Static("/static", "./static/web/")
	router.StaticFS("/assets", http.FS(assets.LoadFile()))

	tmpl := template.Must(template.New("").ParseFS(assets.WebFiles, "web/*.html"))
	router.SetHTMLTemplate(tmpl)

	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})
	router.GET("/favicon.ico", func(c *gin.Context) {
		iconBytes, err := assets.WebFiles.ReadFile("web/favicon.ico")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "image/x-icon", iconBytes)
	})

	err = runGinRouter(router, "0.0.0.0:7845")
	if err != nil {
		log.Printf("web server stopped: %v", err)
		return
	}
}
