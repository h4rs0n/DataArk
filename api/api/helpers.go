package api

import (
	"DataArk/auth"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// helpers.go 提供鉴权、分页参数解析与 JSON 列表序列化辅助函数。

func marshalStringList(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
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

func requireOwner(c *gin.Context) bool {
	user, ok := GetCurrentUser(c)
	if !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"Status": "0", "Message": "请先登录"})
		return false
	}
	if user.Role != auth.UserRoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"Status": "0", "Message": "仅 owner 可执行此操作"})
		return false
	}
	return true
}
