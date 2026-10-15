package api

import (
	"DataArk/assessment"
	"errors"
	"gorm.io/gorm"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// assessment.go 提供 LLM 评估队列、回填回滚与评估指标 HTTP 接口。

// GetAssessmentQueue 返回暂停中的 LLM 评估队列快照；评估被延迟到查重构建完成时
// 一并返回构建中的去重运行，供面板显示持久进度。
func GetAssessmentQueue(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	snapshot, dedupeRun, err := getAssessmentQueue(c.Request.Context(), queryInt(c, "limit", 50))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "查询评估任务队列失败", "Error": err.Error()})
		return
	}
	decorated, err := assessment.DecorateQueue(c.Request.Context(), snapshot)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "查询 Batch 进度失败"})
		return
	}
	if dedupeRun != nil {
		c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询评估任务队列成功", "Data": gin.H{"snapshot": decorated, "dedupeRun": dedupeRun}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "查询评估任务队列成功", "Data": decorated})
}

// RunAssessmentQueue 登记待评估文章并手动启动一次 LLM 评估消费。查重未生效时会
// 先自动构建并应用全库去重运行，评估在该运行生效后自动开始，无需单独请求去重 API。
func RunAssessmentQueue(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	snapshot, dedupeRun, err := runAssessmentQueue(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "启动评估任务队列失败", "Error": err.Error()})
		return
	}
	decorated, err := assessment.DecorateQueue(c.Request.Context(), snapshot)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"Status": "0", "Message": "查询 Batch 进度失败"})
		return
	}
	if dedupeRun != nil {
		c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Message": "全库查重构建中，完成后自动开始评估", "Data": gin.H{"snapshot": decorated, "dedupeRun": dedupeRun}})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Message": "评估任务队列已开始执行", "Data": decorated})
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

func GetAssessmentBatches(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	before, err := strconv.ParseUint(c.DefaultQuery("beforeId", "0"), 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"Status": "0", "Message": "无效的批次游标"})
		return
	}
	rows, err := assessment.ListBatches(c.Request.Context(), queryInt(c, "limit", 20), uint(before))
	if err != nil {
		c.JSON(503, gin.H{"Status": "0", "Message": "查询 Batch 记录失败"})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Data": rows})
}
func GetAssessmentBatch(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	after, e := strconv.ParseUint(c.DefaultQuery("afterId", "0"), 10, 32)
	if err != nil || e != nil || id == 0 {
		c.JSON(400, gin.H{"Status": "0", "Message": "无效的批次或游标"})
		return
	}
	detail, err := assessment.GetBatchDetail(c.Request.Context(), uint(id), queryInt(c, "limit", 50), uint(after))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"Status": "0", "Message": "批次不存在"})
		return
	}
	if err != nil {
		c.JSON(503, gin.H{"Status": "0", "Message": "查询 Batch 失败项失败"})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Data": detail})
}

// ResumeAssessmentBatchCleanup retries only provider file cleanup (owner).
func ResumeAssessmentBatchCleanup(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	if err := resumeAssessmentBatchCleanup(c.Request.Context()); err != nil {
		c.JSON(503, gin.H{"Status": "0", "Message": "恢复 Batch 文件清理失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Message": "Batch 文件清理已重新登记"})
}
