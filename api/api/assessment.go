package api

import (
	"DataArk/assessment"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// assessment.go 提供 LLM 评估队列、回填回滚与评估指标 HTTP 接口。

// GetAssessmentQueue 返回暂停中的 LLM 评估队列快照。
func GetAssessmentQueue(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	snapshot, err := getAssessmentQueue(c.Request.Context(), queryInt(c, "limit", 50))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "查询评估任务队列失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询评估任务队列成功", "Data": snapshot})
}

// RunAssessmentQueue 登记待评估文章并手动启动一次 LLM 评估消费。
func RunAssessmentQueue(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	snapshot, err := runAssessmentQueue(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "启动评估任务队列失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Message": "评估任务队列已开始执行", "Data": snapshot})
}

// BackfillArticleAssessments 准备或入队 article assessment 回填（owner）。
func BackfillArticleAssessments(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	var options assessment.ArticleAssessmentBatchOptions
	if err := c.ShouldBindJSON(&options); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "请输入有效的 assessment 回填参数", "Error": err.Error()})
		return
	}
	result, err := prepareArticleAssessmentBackfill(c.Request.Context(), options)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "准备 article assessment 回填失败", "Error": err.Error(), "Data": result})
		return
	}
	status := http.StatusAccepted
	message := "article assessment 回填已加入队列"
	if options.DryRun {
		status = http.StatusOK
		message = "article assessment 回填预检完成"
	}
	c.JSON(status, gin.H{"Status": "1", "Message": message, "Data": result})
}

// RollbackArticleAssessments 回滚 article assessment（owner）。
func RollbackArticleAssessments(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	var options assessment.ArticleAssessmentBatchOptions
	if err := c.ShouldBindJSON(&options); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "请输入有效的 assessment 回滚参数", "Error": err.Error()})
		return
	}
	result, err := rollbackArticleAssessments(c.Request.Context(), options)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "article assessment 回滚失败", "Error": err.Error(), "Data": result})
		return
	}
	status := http.StatusAccepted
	message := "article assessment 回滚完成"
	if options.DryRun {
		status = http.StatusOK
		message = "article assessment 回滚预检完成"
	}
	c.JSON(status, gin.H{"Status": "1", "Message": message, "Data": result})
}

// GetAssessmentMetrics 返回 owner 评估面板所需的队列深度、耗时与 token 聚合。
func GetAssessmentMetrics(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	metrics, err := getAssessmentMetrics(time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "查询评估指标失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询评估指标成功", "Data": metrics})
}
