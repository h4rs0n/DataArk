package api

import (
	"DataArk/discovery"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// discovery.go 提供内容源、站点、黑名单、爬取队列与候选已读归档 HTTP 接口。

// ListDiscoverySources 列出发现内容源。
func ListDiscoverySources(c *gin.Context) {
	sources, err := listDiscoverySources()
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询内容源失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询内容源成功", "Data": sources})
}

// CreateDiscoverySource 创建发现内容源（owner）。
func CreateDiscoverySource(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
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

// UpdateDiscoverySource 更新发现内容源（owner）。
func UpdateDiscoverySource(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
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

// DeleteDiscoverySource 删除发现内容源（owner）。
func DeleteDiscoverySource(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
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

// FetchDiscoverySource 立即抓取指定内容源（owner）。
func FetchDiscoverySource(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
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

// GetDiscoverySiteGraph 返回站点 blogroll 图谱。
func GetDiscoverySiteGraph(c *gin.Context) {
	siteID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	graph, err := getDiscoverySiteGraph(siteID)
	if err != nil {
		status := http.StatusInternalServerError
		if discovery.IsSiteGraphNotFound(err) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"Status": "0", "Message": "查询站点图谱失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询站点图谱成功", "Data": graph})
}

// GetDiscoveryBackfillCoverage 返回站点历史回填覆盖。
func GetDiscoveryBackfillCoverage(c *gin.Context) {
	siteID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	coverage, err := listBackfillCoverage(siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询历史覆盖失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询历史覆盖成功", "Data": coverage})
}

// UpdateDiscoverySiteStatus 更新站点运营状态（owner）。
func UpdateDiscoverySiteStatus(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	siteID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "请求参数错误"})
		return
	}
	site, err := updateDiscoverySiteStatus(siteID, req.Status, req.Reason)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "更新站点状态失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "站点状态已更新", "Data": site})
}

// RequestDiscoverySiteBackfill 请求站点历史回填（owner）。
func RequestDiscoverySiteBackfill(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	siteID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	if err := requestDiscoverySiteBackfill(c.Request.Context(), siteID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "启动历史回溯失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Message": "历史回溯已排队"})
}

// GetDiscoverySiteOperations 返回站点运营快照。
func GetDiscoverySiteOperations(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	siteID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	operations, err := getDiscoverySiteOperations(siteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询站点运营状态失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询站点运营状态成功", "Data": operations})
}

// GetDiscoveryCrawlQueue 返回爬取任务队列快照（owner）。
func GetDiscoveryCrawlQueue(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	snapshot, err := getDiscoveryCrawlQueue(c.Request.Context(), queryInt(c, "limit", 50))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "查询爬取任务队列失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询爬取任务队列成功", "Data": snapshot})
}

// RunDiscoveryCrawlQueue 登记到期爬取任务（owner）。
func RunDiscoveryCrawlQueue(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	snapshot, err := runDiscoveryCrawlQueue(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "登记到期爬取任务失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Message": "到期爬取任务已登记，将自动执行", "Data": snapshot})
}

// ListDiscoveryDomainBlacklist 列出域名黑名单（owner）。
func ListDiscoveryDomainBlacklist(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	entries, err := listDiscoveryDomainBlacklist()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询域名黑名单失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询域名黑名单成功", "Data": entries})
}

// CreateDiscoveryDomainBlacklist 添加域名黑名单（owner）。
func CreateDiscoveryDomainBlacklist(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	var req struct {
		Domain string `json:"domain" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "请输入有效域名"})
		return
	}
	mutation, err := createDiscoveryDomainBlacklist(req.Domain, req.Reason)
	if err != nil {
		status := http.StatusInternalServerError
		message := "添加域名黑名单失败"
		if errors.Is(err, discovery.ErrInvalidDiscoveryBlacklistDomain) {
			status, message = http.StatusBadRequest, "域名格式无效"
		} else if errors.Is(err, discovery.ErrDuplicateDiscoveryBlacklistDomain) {
			status, message = http.StatusConflict, "该域名已在黑名单中"
		}
		c.JSON(status, gin.H{"Status": "0", "Message": message, "Error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"Status": "1", "Message": "域名黑名单已添加", "Data": mutation})
}

// DeleteDiscoveryDomainBlacklist 删除域名黑名单（owner）。
func DeleteDiscoveryDomainBlacklist(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	mutation, err := deleteDiscoveryDomainBlacklist(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"Status": "0", "Message": "域名黑名单规则不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "删除域名黑名单失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "域名黑名单已删除", "Data": mutation})
}

// MarkDiscoveryCandidateRead 将发现候选标记为已读。
func MarkDiscoveryCandidateRead(c *gin.Context) {
	userID, authenticated := requireCurrentUserID(c)
	if !authenticated {
		return
	}
	candidateID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	candidate, err := markCandidateRead(userID, candidateID)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "更新候选文章失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "候选文章已标记阅读", "Data": candidate})
}

// ArchiveDiscoveryCandidate 将发现候选归档到用户库。
func ArchiveDiscoveryCandidate(c *gin.Context) {
	userID, authenticated := requireCurrentUserID(c)
	if !authenticated {
		return
	}
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
	updatedCandidate, err := markCandidateArchived(userID, candidateID, task.ID)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "更新候选文章失败", "Error": err.Error()})
		return
	}
	c.JSON(202, gin.H{"Status": "1", "Message": "候选文章已加入归档队列", "Data": gin.H{"candidate": updatedCandidate, "task": task}})
}

type discoverySourceRequest struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}
