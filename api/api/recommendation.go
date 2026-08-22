package api

import (
	"DataArk/recommendation"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// recommendation.go 提供日报、发现流、设置、反馈与库存 HTTP 接口。

// GetRecommendationToday 返回当前用户今日推荐日报。
func GetRecommendationToday(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	date, err := recommendationDateForUser(userID, recommendationNow())
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "计算用户本地日期失败", "Error": err.Error()})
		return
	}
	snapshot, err := getRecommendationDaySnapshot(userID, date)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询今日推荐失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询今日推荐成功", "Data": snapshot})
}

// GetRecommendationTodaySummary 返回今日日报摘要。
func GetRecommendationTodaySummary(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	date, err := recommendationDateForUser(userID, recommendationNow())
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "计算用户本地日期失败", "Error": err.Error()})
		return
	}
	writeRecommendationDaySummary(c, userID, date)
}

// GetRecommendationDaySummary 按路径中的日期返回该日推荐总结，供每日推荐页翻阅历史日报。
func GetRecommendationDaySummary(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	date := strings.TrimSpace(c.Param("date"))
	if date == "" {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "缺少日报日期"})
		return
	}
	writeRecommendationDaySummary(c, userID, date)
}

// writeRecommendationDaySummary 生成或读取指定日期的日报总结并写入 JSON 响应。
func writeRecommendationDaySummary(c *gin.Context, userID uint, date string) {
	summary, err := getRecommendationDaySummary(c.Request.Context(), userID, date)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "生成推荐总结失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询推荐总结成功", "Data": summary})
}

// GetDiscoveryRecommendationFeed 返回发现推荐流。
func GetDiscoveryRecommendationFeed(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	snapshot, err := getCurrentDiscoveryFeed(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询猜你喜欢失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询猜你喜欢成功", "Data": snapshot})
}

// RefreshDiscoveryRecommendationFeed 刷新发现推荐流。
func RefreshDiscoveryRecommendationFeed(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	snapshot, err := refreshDiscoveryFeed(c.Request.Context(), userID, 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "刷新猜你喜欢失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"Status": "1", "Message": "猜你喜欢已更新", "Data": snapshot})
}

// GetRecommendationHistory 分页列出历史日报。
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

// GetRecommendationDay 返回指定日期日报快照。
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

// GenerateRecommendationDay 生成指定日期日报（owner）。
func GenerateRecommendationDay(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	snapshot, err := regenerateRecommendations(c.Request.Context(), userID, c.Query("date"))
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "生成推荐日报失败", "Error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"Status": "1", "Message": "推荐日报已生成", "Data": snapshot})
}

// SupplementRecommendationDay 为缺篇日报补文（owner）。
func SupplementRecommendationDay(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	snapshot, err := supplementRecommendations(c.Request.Context(), userID, c.Query("date"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "补充推荐日报失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "推荐日报补充完成", "Data": snapshot})
}

// GetRecommendationSettings 读取当前用户推荐设置。
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

// GetRecommendationInventory 返回当前用户候选库存。
func GetRecommendationInventory(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	inventory, err := getCandidateInventory(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询候选库存失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询候选库存成功", "Data": inventory})
}

// GetAdminProductMetrics 返回产品运营指标（owner）。
func GetAdminProductMetrics(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	metrics, err := getAdminProductMetrics(time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询产品运营指标失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询产品运营指标成功", "Data": metrics})
}

// UpdateRecommendationSettings 更新当前用户推荐设置。
func UpdateRecommendationSettings(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	var req struct {
		DailyLimit          int      `json:"dailyLimit"`
		Timezone            string   `json:"timezone"`
		GenerationTime      string   `json:"generationTime"`
		CandidateWindowDays int      `json:"candidateWindowDays"`
		ExplorationRate     float64  `json:"explorationRate"`
		PreferredTopics     []string `json:"preferredTopics"`
		PreferredLanguages  []string `json:"preferredLanguages"`
		PreferredLength     string   `json:"preferredLength"`
		PreferredDepth      float64  `json:"preferredDepth"`
		FavoriteSources     []string `json:"favoriteSources"`
		Enabled             bool     `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(403, gin.H{"Status": "0", "Message": "请求参数错误"})
		return
	}
	settings, err := saveRecommendationSettings(&recommendation.RecommendationSettings{
		UserID:              userID,
		DailyLimit:          req.DailyLimit,
		Timezone:            req.Timezone,
		GenerationTime:      req.GenerationTime,
		CandidateWindowDays: req.CandidateWindowDays,
		ExplorationRate:     req.ExplorationRate,
		PreferredTopics:     marshalStringList(req.PreferredTopics),
		PreferredLanguages:  marshalStringList(req.PreferredLanguages),
		PreferredLength:     req.PreferredLength,
		PreferredDepth:      req.PreferredDepth,
		FavoriteSources:     marshalStringList(req.FavoriteSources),
		Enabled:             req.Enabled,
	})
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "更新推荐设置失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "更新推荐设置成功", "Data": settings})
}

// RecordRecommendationItemFeedback 记录推荐条目反馈。
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
		Action       string                                     `json:"action"`
		BlockTargets []recommendation.RecommendationBlockTarget `json:"blockTargets"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(403, gin.H{"Status": "0", "Message": "请求参数错误"})
		return
	}
	feedback, rules, err := recordRecommendationFeedback(userID, itemID, req.Action, req.BlockTargets)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, recommendation.ErrInvalidRecommendationFeedback) || errors.Is(err, recommendation.ErrInvalidBlockRule) {
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"Status": "0", "Message": "记录推荐反馈失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "推荐反馈已记录", "Data": gin.H{"feedback": feedback, "blockRules": rules}})
}

// RevertRecommendationItemFeedback 撤销推荐条目当前反馈。
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

// GetRecommendationItemFeedback 读取条目当前反馈与历史。
func GetRecommendationItemFeedback(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	itemID, ok := parseUintParam(c, "itemId")
	if !ok {
		return
	}
	feedback, err := getCurrentRecommendationFeedback(userID, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询当前反馈失败", "Error": err.Error()})
		return
	}
	history, err := listRecommendationFeedbackHistory(userID, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询反馈历史失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询推荐反馈成功", "Data": gin.H{"current": feedback, "history": history}})
}

// GetRecommendationItemContext 返回推荐条目可追溯上下文。
func GetRecommendationItemContext(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	itemID, ok := parseUintParam(c, "itemId")
	if !ok {
		return
	}
	context, err := getRecommendationItemContext(userID, itemID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"Status": "0", "Message": "查询推荐追溯信息失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询推荐追溯信息成功", "Data": context})
}

// ResetRecommendationPreferences 重置用户推荐偏好。
func ResetRecommendationPreferences(c *gin.Context) {
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	profile, err := resetUserRecommendationPreferences(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "重置推荐偏好失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "推荐偏好已重置", "Data": profile})
}

// ListRecommendationBlocks 列出用户屏蔽规则。
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

// DeleteRecommendationBlock 停用一条屏蔽规则。
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
