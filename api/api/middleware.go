package api

import (
	"EchoArk/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

// AuthMiddleware JWT认证中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从请求头中获取Authorization
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"Status":  "0",
				"Message": "Please provide a valid token",
			})
			c.Abort()
			return
		}

		// 提取Token
		tokenString, err := common.ExtractTokenFromHeader(authHeader)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"Status":  "0",
				"Message": "Please provide a valid token",
				"Error":   err.Error(),
			})
			c.Abort()
			return
		}

		// 验证Token
		claims, err := common.ValidateToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"Status":  "0",
				"Message": "Please provide a valid token",
				"Error":   err.Error(),
			})
			c.Abort()
			return
		}

		// 获取用户信息
		user, err := common.GetUserByID(claims.UserID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"Status":  "0",
				"Message": "The user associated with this token does not exist",
			})
			c.Abort()
			return
		}

		// 将用户信息存储在上下文中，供后续处理器使用
		c.Set("user", user)
		c.Set("user_id", user.ID)
		c.Set("username", user.Username)
		c.Set("claims", claims)

		// 继续处理请求
		c.Next()
	}
}

// OptionalAuthMiddleware 可选的认证中间件（用于某些接口可登录可不登录）
func OptionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// 没有Token，继续处理但不设置用户信息
			c.Next()
			return
		}

		tokenString, err := common.ExtractTokenFromHeader(authHeader)
		if err != nil {
			// Token格式错误，继续处理但不设置用户信息
			c.Next()
			return
		}

		claims, err := common.ValidateToken(tokenString)
		if err != nil {
			// Token无效，继续处理但不设置用户信息
			c.Next()
			return
		}

		user, err := common.GetUserByID(claims.UserID)
		if err != nil {
			// 用户不存在，继续处理但不设置用户信息
			c.Next()
			return
		}

		// 设置用户信息到上下文
		c.Set("user", user)
		c.Set("user_id", user.ID)
		c.Set("username", user.Username)
		c.Set("claims", claims)
		c.Set("is_authenticated", true)

		c.Next()
	}
}

// GetCurrentUser 从上下文中获取当前用户
func GetCurrentUser(c *gin.Context) (*common.User, bool) {
	if user, exists := c.Get("user"); exists {
		if u, ok := user.(*common.User); ok {
			return u, true
		}
	}
	return nil, false
}

// GetCurrentUserID 从上下文中获取当前用户ID
func GetCurrentUserID(c *gin.Context) (uint, bool) {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(uint); ok {
			return id, true
		}
	}
	return 0, false
}

// GetCurrentUsername 从上下文中获取当前用户名
func GetCurrentUsername(c *gin.Context) (string, bool) {
	if username, exists := c.Get("username"); exists {
		if name, ok := username.(string); ok {
			return name, true
		}
	}
	return "", false
}

// IsAuthenticated 检查用户是否已认证
func IsAuthenticated(c *gin.Context) bool {
	_, exists := c.Get("user")
	return exists
}

// RequireAuth 检查用户是否已认证，未认证则返回错误
func RequireAuth(c *gin.Context) (*common.User, bool) {
	user, exists := GetCurrentUser(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "Authentication required",
			"code":    "AUTH_REQUIRED",
			"message": "This endpoint requires authentication",
		})
		return nil, false
	}
	return user, true
}
