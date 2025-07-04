package common

import (
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"strconv"
	"time"
)

// JWT配置
var (
	// JWT密钥，生产环境中应该从环境变量或配置文件中读取
	jwtSecret = []byte(MEILIAPIKey)
	// Token过期时间
	tokenExpiration = time.Hour * 24 * 7 // 7天
)

// Claims JWT载荷结构
type Claims struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

// TokenResponse 返回给客户端的Token结构
type TokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      *User     `json:"user"`
}

// SetTokenExpiration 设置Token过期时间
func SetTokenExpiration(duration time.Duration) {
	tokenExpiration = duration
}

// GenerateToken 生成JWT Token
func GenerateToken(user *User) (*TokenResponse, error) {
	if user == nil {
		return nil, errors.New("user cannot be nil")
	}

	// 设置过期时间
	expirationTime := time.Now().Add(tokenExpiration)

	// 创建Claims
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "your-app-name",
			Subject:   strconv.Itoa(int(user.ID)),
		},
	}

	// 创建Token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 签名Token
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %v", err)
	}

	return &TokenResponse{
		Token:     tokenString,
		ExpiresAt: expirationTime,
		User:      user,
	}, nil
}

// ValidateToken 验证JWT Token
func ValidateToken(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, errors.New("token is required")
	}

	// 解析Token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %v", err)
	}

	// 验证Token是否有效
	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	// 获取Claims
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// RefreshToken 刷新Token
func RefreshToken(tokenString string) (*TokenResponse, error) {
	// 验证当前Token
	claims, err := ValidateToken(tokenString)
	if err != nil {
		return nil, fmt.Errorf("invalid token for refresh: %v", err)
	}

	// 检查Token是否即将过期（在30分钟内过期才允许刷新）
	if time.Until(claims.ExpiresAt.Time) > 30*time.Minute {
		return nil, errors.New("token is still valid, refresh not needed")
	}

	// 根据用户ID获取最新的用户信息
	user, err := GetUserByID(claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %v", err)
	}

	// 生成新Token
	return GenerateToken(user)
}

// ExtractTokenFromHeader 从Authorization header中提取Token
func ExtractTokenFromHeader(authHeader string) (string, error) {
	if authHeader == "" {
		return "", errors.New("authorization header is required")
	}

	// 检查Bearer前缀
	const bearerPrefix = "Bearer "
	if len(authHeader) <= len(bearerPrefix) || authHeader[:len(bearerPrefix)] != bearerPrefix {
		return "", errors.New("invalid authorization header format")
	}

	return authHeader[len(bearerPrefix):], nil
}

// GetUserFromToken 从Token中获取用户信息
func GetUserFromToken(tokenString string) (*User, error) {
	claims, err := ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}

	user, err := GetUserByID(claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user from database: %v", err)
	}

	return user, nil
}

// IsTokenExpired 检查Token是否过期
func IsTokenExpired(tokenString string) bool {
	claims, err := ValidateToken(tokenString)
	if err != nil {
		return true
	}

	return time.Now().After(claims.ExpiresAt.Time)
}

// GetTokenRemainingTime 获取Token剩余有效时间
func GetTokenRemainingTime(tokenString string) (time.Duration, error) {
	claims, err := ValidateToken(tokenString)
	if err != nil {
		return 0, err
	}

	remaining := time.Until(claims.ExpiresAt.Time)
	if remaining < 0 {
		return 0, errors.New("token has expired")
	}

	return remaining, nil
}

// RevokeToken 撤销Token（在实际应用中，你可能需要维护一个黑名单）
func RevokeToken(tokenString string) error {
	// 验证Token是否有效
	_, err := ValidateToken(tokenString)
	if err != nil {
		return fmt.Errorf("invalid token: %v", err)
	}

	// 在实际应用中，你应该将Token添加到黑名单中
	// 这里只是一个示例，实际实现可能需要Redis或数据库来存储黑名单
	// 例如：addToBlacklist(tokenString)

	return nil
}

// LoginWithToken 使用用户名密码登录并生成Token
func LoginWithToken(username, password string) (*TokenResponse, error) {
	// 验证用户登录
	user, err := LoginUser(username, password)
	if err != nil {
		return nil, err
	}

	// 生成Token
	tokenResponse, err := GenerateToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %v", err)
	}

	return tokenResponse, nil
}

// RegisterWithToken 用户注册并生成Token
func RegisterWithToken(username, password string) (*TokenResponse, error) {
	// 创建用户
	user, err := CreateUser(username, password)
	if err != nil {
		return nil, err
	}

	// 生成Token
	tokenResponse, err := GenerateToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %v", err)
	}

	return tokenResponse, nil
}
