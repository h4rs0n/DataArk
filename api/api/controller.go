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
	"time"
)

var (
	checkArchiveConsistency      = search.CheckArchiveConsistency
	repairArchiveConsistency     = search.RepairArchiveConsistency
	registerWithToken            = common.RegisterWithToken
	loginWithToken               = common.LoginWithToken
	queryByKeyword               = search.QueryByKeyword
	addDocURLTask                = search.AddDocURLTask
	getArchiveTask               = search.GetArchiveTask
	getArchiveStatsSnapshot      = common.GetArchiveStats
	refreshStatsFromDisk         = common.RefreshArchiveStatsFromDisk
	recordSearchEvent            = common.RecordSearchEvent
	getKeywordStats              = common.GetKeywordStats
	recordArchiveClick           = common.RecordArchiveClick
	getArchiveRankings           = common.GetArchiveRankings
	getArchiveRecommendations    = common.GetArchiveRecommendations
	listDiscoverySources         = common.ListDiscoverySources
	createDiscoverySource        = common.CreateDiscoverySource
	updateDiscoverySource        = common.UpdateDiscoverySource
	deleteDiscoverySource        = common.DeleteDiscoverySource
	fetchDiscoverySourceByID     = common.FetchDiscoverySourceByID
	listDiscoveryCandidates      = common.ListDiscoveryCandidates
	getDiscoveryCandidate        = common.GetDiscoveryCandidate
	markCandidateRead            = common.MarkDiscoveryCandidateRead
	markCandidateIgnored         = common.MarkDiscoveryCandidateIgnored
	markCandidateArchived        = common.MarkDiscoveryCandidateArchived
	getRecommendationSettings    = common.GetRecommendationSettings
	saveRecommendationSettings   = common.SaveRecommendationSettings
	getRecommendationDaySnapshot = common.GetRecommendationDaySnapshot
	listRecommendationDays       = common.ListRecommendationDays
	createRecommendationDay      = common.CreateRecommendationDay
	generateDailyRecommendations = common.GenerateDailyRecommendations
	recordRecommendationFeedback = common.RecordRecommendationFeedback
	revertRecommendationFeedback = common.RevertRecommendationFeedback
	listUserBlockRules           = common.ListUserBlockRules
	deleteUserBlockRule          = common.DeleteUserBlockRule
	startDiscoveryScheduler      = common.StartDiscoveryScheduler
	startRecommendationScheduler = common.StartRecommendationScheduler
	addDocFileToIndex            = search.AddDocFile
	deleteDocByHTMLPath          = search.DeleteDocByHTMLPath
	createBackupArchive          = backup.CreateBackup
	restoreBackupArchive         = backup.RestoreBackup
	initDatabase                 = common.InitDB
	createSearchIndex            = search.CreateDefaultIndex
	initArchiveQueue             = search.InitArchiveTaskQueue
	runGinRouter                 = func(router *gin.Engine, addr string) error {
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
	_ = recordSearchEvent(queryString, pageAndHits["TotalHits"])

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

func GetSearchKeywords(c *gin.Context) {
	limit := queryInt(c, "limit", 10)
	stats, err := getKeywordStats(c.Query("prefix"), c.DefaultQuery("window", "7d"), limit)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询搜索关键词失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询搜索关键词成功", "Data": stats})
}

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

func GetArchiveRankings(c *gin.Context) {
	rankings, err := getArchiveRankings(c.DefaultQuery("window", "7d"), queryInt(c, "limit", 20))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询点击排行失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询点击排行成功", "Data": rankings})
}

func GetArchiveRecommendations(c *gin.Context) {
	recommendations, err := getArchiveRecommendations(c.DefaultQuery("window", "7d"), queryInt(c, "limit", 20))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询归档推荐失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询归档推荐成功", "Data": recommendations})
}

func ListDiscoverySources(c *gin.Context) {
	sources, err := listDiscoverySources()
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询内容源失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询内容源成功", "Data": sources})
}

func CreateDiscoverySource(c *gin.Context) {
	var req discoverySourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(403, gin.H{"Status": "0", "Message": "请求参数错误"})
		return
	}
	source, err := createDiscoverySource(req.Name, req.URL, req.Type, req.Enabled)
	if err != nil {
		c.JSON(403, gin.H{"Status": "0", "Message": "创建内容源失败", "Error": err.Error()})
		return
	}
	c.JSON(201, gin.H{"Status": "1", "Message": "创建内容源成功", "Data": source})
}

func UpdateDiscoverySource(c *gin.Context) {
	sourceID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req discoverySourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(403, gin.H{"Status": "0", "Message": "请求参数错误"})
		return
	}
	source, err := updateDiscoverySource(sourceID, req.Name, req.URL, req.Type, req.Enabled)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "更新内容源失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "更新内容源成功", "Data": source})
}

func DeleteDiscoverySource(c *gin.Context) {
	sourceID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	if err := deleteDiscoverySource(sourceID); err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "删除内容源失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "删除内容源成功"})
}

func FetchDiscoverySource(c *gin.Context) {
	sourceID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	result, err := fetchDiscoverySourceByID(c.Request.Context(), sourceID)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "刷新内容源失败", "Error": err.Error(), "Data": result})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "刷新内容源成功", "Data": result})
}

func ListDiscoveryCandidates(c *gin.Context) {
	candidates, err := listDiscoveryCandidates(c.Query("status"), queryInt(c, "limit", 50))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询候选文章失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询候选文章成功", "Data": candidates})
}

func MarkDiscoveryCandidateRead(c *gin.Context) {
	candidateID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	candidate, err := markCandidateRead(candidateID)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "更新候选文章失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "候选文章已标记阅读", "Data": candidate})
}

func IgnoreDiscoveryCandidate(c *gin.Context) {
	candidateID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	candidate, err := markCandidateIgnored(candidateID)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "忽略候选文章失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "候选文章已忽略", "Data": candidate})
}

func ArchiveDiscoveryCandidate(c *gin.Context) {
	candidateID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	candidate, err := getDiscoveryCandidate(candidateID)
	if err != nil {
		c.JSON(404, gin.H{"Status": "0", "Message": "候选文章不存在", "Error": err.Error()})
		return
	}
	task, _, err := addDocURLTask(candidate.URL)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "创建归档任务失败", "Error": err.Error()})
		return
	}
	updatedCandidate, err := markCandidateArchived(candidateID, task.ID)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "更新候选文章失败", "Error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"Status": "1", "Message": "候选文章已加入归档队列", "Data": gin.H{"candidate": updatedCandidate, "task": task}})
}

func GetRecommendationToday(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	snapshot, err := getRecommendationDaySnapshot(userID, time.Now().Format("2006-01-02"))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询今日推荐失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询今日推荐成功", "Data": snapshot})
}

func GetRecommendationHistory(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	days, err := listRecommendationDays(userID, c.Query("from"), c.Query("to"), queryInt(c, "page", 1), queryInt(c, "pageSize", 20))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询历史推荐失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询历史推荐成功", "Data": days})
}

func GetRecommendationDay(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	snapshot, err := getRecommendationDaySnapshot(userID, c.Param("date"))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询推荐日报失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询推荐日报成功", "Data": snapshot})
}

func GenerateRecommendationDay(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	snapshot, err := generateDailyRecommendations(c.Request.Context(), userID, c.Query("date"))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "生成推荐日报失败", "Error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"Status": "1", "Message": "推荐日报已生成", "Data": snapshot})
}

func GetRecommendationSettings(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	settings, err := getRecommendationSettings(userID)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询推荐设置失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询推荐设置成功", "Data": settings})
}

func UpdateRecommendationSettings(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		DailyLimit          int     `json:"dailyLimit"`
		Timezone            string  `json:"timezone"`
		GenerationTime      string  `json:"generationTime"`
		CandidateWindowDays int     `json:"candidateWindowDays"`
		ExplorationRate     float64 `json:"explorationRate"`
		Enabled             bool    `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(403, gin.H{"Status": "0", "Message": "请求参数错误"})
		return
	}
	settings, err := saveRecommendationSettings(&common.RecommendationSettings{
		UserID:              userID,
		DailyLimit:          req.DailyLimit,
		Timezone:            req.Timezone,
		GenerationTime:      req.GenerationTime,
		CandidateWindowDays: req.CandidateWindowDays,
		ExplorationRate:     req.ExplorationRate,
		Enabled:             req.Enabled,
	})
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "更新推荐设置失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "更新推荐设置成功", "Data": settings})
}

func RecordRecommendationItemFeedback(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	itemID, ok := parseUintParam(c, "itemId")
	if !ok {
		return
	}
	var req struct {
		Action       string                             `json:"action"`
		BlockTargets []common.RecommendationBlockTarget `json:"blockTargets"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(403, gin.H{"Status": "0", "Message": "请求参数错误"})
		return
	}
	feedback, rules, err := recordRecommendationFeedback(userID, itemID, req.Action, req.BlockTargets)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, common.ErrInvalidRecommendationFeedback) || errors.Is(err, common.ErrInvalidBlockRule) {
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"Status": "0", "Message": "记录推荐反馈失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "推荐反馈已记录", "Data": gin.H{"feedback": feedback, "blockRules": rules}})
}

func RevertRecommendationItemFeedback(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	itemID, ok := parseUintParam(c, "itemId")
	if !ok {
		return
	}
	if err := revertRecommendationFeedback(userID, itemID); err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "撤销推荐反馈失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "推荐反馈已撤销"})
}

func ListRecommendationBlocks(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	rules, err := listUserBlockRules(userID, true)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询屏蔽规则失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询屏蔽规则成功", "Data": rules})
}

func DeleteRecommendationBlock(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	ruleID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	if err := deleteUserBlockRule(userID, ruleID); err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "删除屏蔽规则失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "屏蔽规则已删除"})
}

type discoverySourceRequest struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

func queryInt(c *gin.Context, key string, defaultValue int) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return defaultValue
	}
	parsedValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsedValue
}

func parseUintParam(c *gin.Context, key string) (uint, bool) {
	rawValue := strings.TrimSpace(c.Param(key))
	parsedValue, err := strconv.ParseUint(rawValue, 10, 64)
	if err != nil || parsedValue == 0 {
		c.JSON(403, gin.H{"Status": "0", "Message": "编号参数错误"})
		return 0, false
	}
	return uint(parsedValue), true
}

func requireCurrentUserID(c *gin.Context) (uint, bool) {
	userID, ok := GetCurrentUserID(c)
	if !ok || userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"Status": "0", "Message": "请先登录"})
		return 0, false
	}
	return userID, true
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
	Domain    string `json:"domain"`
	SourceURL string `json:"sourceUrl"`
	Files     []File `json:"files"`
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
	stopDiscoveryScheduler := startDiscoveryScheduler()
	defer stopDiscoveryScheduler()
	stopRecommendationScheduler := startRecommendationScheduler()
	defer stopRecommendationScheduler()
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
		protected.GET("/search/keywords", GetSearchKeywords)
		protected.POST("/uploadHtmlFile", AddHTMLFile)
		protected.POST("/upload", AddDocByHTMLFile)
		protected.POST("/archiveByURL", AddDocByURL)
		protected.GET("/archiveTask/:taskId", GetArchiveTaskStatus)
		protected.POST("/archive/clicks", RecordArchiveClick)
		protected.GET("/archive/rankings", GetArchiveRankings)
		protected.GET("/recommendations/archives", GetArchiveRecommendations)
		protected.GET("/archiveStats", GetArchiveStats)
		protected.POST("/archiveStats/refresh", RefreshArchiveStats)
		protected.GET("/archiveConsistency", GetArchiveConsistency)
		protected.POST("/archiveConsistency/repair", RepairArchiveConsistency)
		protected.DELETE("/archive", DeleteArchiveDocument)
		protected.GET("/discovery/sources", ListDiscoverySources)
		protected.POST("/discovery/sources", CreateDiscoverySource)
		protected.PUT("/discovery/sources/:id", UpdateDiscoverySource)
		protected.DELETE("/discovery/sources/:id", DeleteDiscoverySource)
		protected.POST("/discovery/sources/:id/fetch", FetchDiscoverySource)
		protected.GET("/discovery/candidates", ListDiscoveryCandidates)
		protected.POST("/discovery/candidates/:id/read", MarkDiscoveryCandidateRead)
		protected.POST("/discovery/candidates/:id/archive", ArchiveDiscoveryCandidate)
		protected.POST("/discovery/candidates/:id/ignore", IgnoreDiscoveryCandidate)
		protected.GET("/recommendations/today", GetRecommendationToday)
		protected.GET("/recommendations/history", GetRecommendationHistory)
		protected.GET("/recommendations/days/:date", GetRecommendationDay)
		protected.POST("/admin/recommendations/generate", GenerateRecommendationDay)
		protected.GET("/recommendations/settings", GetRecommendationSettings)
		protected.PUT("/recommendations/settings", UpdateRecommendationSettings)
		protected.POST("/recommendations/items/:itemId/feedback", RecordRecommendationItemFeedback)
		protected.DELETE("/recommendations/items/:itemId/feedback", RevertRecommendationItemFeedback)
		protected.GET("/recommendations/blocks", ListRecommendationBlocks)
		protected.DELETE("/recommendations/blocks/:id", DeleteRecommendationBlock)
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
	router.GET("/favicon.ico", func(c *gin.Context) {
		iconBytes, err := assets.WebFiles.ReadFile("web/favicon.ico")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "image/x-icon", iconBytes)
	})

	err := runGinRouter(router, "0.0.0.0:7845")
	if err != nil {
		fmt.Print("Maybe the port is already in use. Please check it.")
		return
	}
}
