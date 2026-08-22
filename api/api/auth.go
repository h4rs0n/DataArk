package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// auth.go 提供注册、登录与当前用户校验 HTTP 接口。

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

// AuthChecker 返回当前已登录用户信息。
func (ac *AuthController) AuthChecker(c *gin.Context) {
	user, _ := GetCurrentUser(c)
	c.JSON(http.StatusOK, gin.H{
		"Status":  "1",
		"Message": "Already login",
		"Data":    user,
	})
}
