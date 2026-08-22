package api

import (
	"DataArk/assessment"
	"DataArk/assessmenteval"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// assessment.go 提供 LLM 评估队列、回填回滚、金标工作流与评估指标 HTTP 接口。

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

// GetArticleAssessmentWorkflow 返回金标评估工作流摘要（owner）。
func GetArticleAssessmentWorkflow(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	summary, err := getArticleAssessmentWorkflow()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"Status": "0", "Message": "加载人工标注工作流失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "人工标注工作流加载成功", "Data": summary})
}

// CreateArticleAssessmentWorkflow 创建金标评估工作流（owner）。
func CreateArticleAssessmentWorkflow(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	userID, ok := requireCurrentUserID(c)
	if !ok {
		return
	}
	summary, err := createArticleAssessmentWorkflow(userID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"Status": "0", "Message": "创建人工标注样本失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"Status": "1", "Message": "已创建 120 篇人工标注样本", "Data": summary})
}

// GetArticleAssessmentWorkflowItem 加载盲标文章（owner）。
func GetArticleAssessmentWorkflowItem(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	runID, ok := parseUintParam(c, "runId")
	if !ok {
		return
	}
	passValue, passErr := strconv.Atoi(strings.TrimSpace(c.Param("pass")))
	positionValue, positionErr := strconv.Atoi(strings.TrimSpace(c.Param("position")))
	if passErr != nil || positionErr != nil || passValue < 1 || passValue > 3 || positionValue < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "标注轮次或文章位置无效"})
		return
	}
	item, err := getArticleAssessmentWorkflowItem(runID, passValue, positionValue)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"Status": "0", "Message": "加载盲标文章失败", "Error": err.Error()})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "盲标文章加载成功", "Data": item})
}

// SaveArticleAssessmentWorkflowLabel 保存盲标标签（owner）。
func SaveArticleAssessmentWorkflowLabel(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	userID, authenticated := requireCurrentUserID(c)
	if !authenticated {
		return
	}
	runID, ok := parseUintParam(c, "runId")
	if !ok {
		return
	}
	passValue, err := strconv.Atoi(strings.TrimSpace(c.Param("pass")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "标注轮次无效"})
		return
	}
	var input assessmenteval.WorkflowLabelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "请输入有效的人工评分", "Error": err.Error()})
		return
	}
	summary, err := saveArticleAssessmentWorkflowLabel(runID, userID, passValue, c.Param("sampleId"), input)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"Status": "0", "Message": "保存人工评分失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "人工评分已保存", "Data": summary})
}

// SkipArticleAssessmentWorkflowItem 跳过当前盲标文章（owner）。
func SkipArticleAssessmentWorkflowItem(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	userID, authenticated := requireCurrentUserID(c)
	if !authenticated {
		return
	}
	runID, ok := parseUintParam(c, "runId")
	if !ok {
		return
	}
	passValue, err := strconv.Atoi(strings.TrimSpace(c.Param("pass")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "标注轮次无效"})
		return
	}
	summary, err := skipArticleAssessmentWorkflowItem(runID, userID, passValue, c.Param("sampleId"))
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"Status": "0", "Message": "跳过文章失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "文章已跳过", "Data": summary})
}

// AdvanceArticleAssessmentWorkflow 推进金标工作流轮次（owner）。
func AdvanceArticleAssessmentWorkflow(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	runID, ok := parseUintParam(c, "runId")
	if !ok {
		return
	}
	summary, err := advanceArticleAssessmentWorkflow(runID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"Status": "0", "Message": "当前人工标注阶段无法完成", "Error": err.Error(), "Data": summary})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Message": "人工标注工作流已进入下一阶段", "Data": summary})
}

// EvaluateArticleAssessmentWorkflow 启动金标工作流评估（owner）。
func EvaluateArticleAssessmentWorkflow(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	runID, ok := parseUintParam(c, "runId")
	if !ok {
		return
	}
	summary, err := evaluateArticleAssessmentWorkflow(runID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"Status": "0", "Message": "启动模型验收失败", "Error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Message": "模型双跑验收已在后台启动", "Data": summary})
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
