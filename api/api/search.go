package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// search.go 提供关键词搜索与热词查询 HTTP 接口。

// SearchByKeyword 按关键词分页搜索归档文档。
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

// GetSearchKeywords 返回搜索热词统计。
func GetSearchKeywords(c *gin.Context) {
	limit := queryInt(c, "limit", 10)
	stats, err := getKeywordStats(c.Query("prefix"), c.DefaultQuery("window", "7d"), limit)
	if err != nil {
		c.JSON(500, gin.H{"Status": "0", "Message": "查询搜索关键词失败", "Error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"Status": "1", "Message": "查询搜索关键词成功", "Data": stats})
}
