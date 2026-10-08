package api

import (
	"DataArk/discovery"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func dedupeError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, discovery.ErrDedupeConflict) {
		status = http.StatusConflict
	}
	c.JSON(status, gin.H{"Status": "0", "Message": "文章去重操作失败", "Error": err.Error()})
}
func dedupeID(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || value == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"Status": "0", "Message": "无效的去重运行 ID"})
		return 0, false
	}
	return uint(value), true
}
func CreateDedupeRun(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	run, err := discovery.CreateDedupeRun(c.Request.Context())
	if err != nil {
		dedupeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Message": "全库去重预览已登记", "Data": run})
}
func GetDedupeRun(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	id, ok := dedupeID(c)
	if !ok {
		return
	}
	cursor := queryInt(c, "cursor", 0)
	if cursor < 0 {
		cursor = 0
	}
	report, err := discovery.GetDedupeReport(c.Request.Context(), id, uint(cursor), queryInt(c, "limit", 50))
	if err != nil {
		dedupeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Data": report})
}
func ApplyDedupeRun(c *gin.Context)    { requestDedupeAction(c, "apply") }
func RollbackDedupeRun(c *gin.Context) { requestDedupeAction(c, "rollback") }
func requestDedupeAction(c *gin.Context, action string) {
	if !requireOwner(c) {
		return
	}
	id, ok := dedupeID(c)
	if !ok {
		return
	}
	run, err := discovery.RequestDedupeAction(c.Request.Context(), id, action)
	if err != nil {
		dedupeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"Status": "1", "Data": run})
}
func GetDedupeCluster(c *gin.Context) {
	if !requireOwner(c) {
		return
	}
	cursor := queryInt(c, "cursor", 0)
	if cursor < 0 {
		cursor = 0
	}
	detail, err := discovery.GetDedupeCluster(c.Request.Context(), c.Param("id"), uint(cursor), queryInt(c, "limit", 50))
	if err != nil {
		dedupeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"Status": "1", "Data": detail})
}
