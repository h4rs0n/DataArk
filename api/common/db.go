package common

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"time"
)

var db *gorm.DB

// User 用户模型
type User struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Username  string    `json:"username" gorm:"unique;not null"`
	Password  string    `json:"-" gorm:"not null"` // json:"-" 表示不会在JSON序列化中包含密码
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func InitDB() {
	var err error
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		DBHost, DBUser, DBPassword, DBName, DBPort)
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect to the database", err)
	}
	// fmt.Println("Database connected successfully!")

	// 自动迁移用户表
	err = db.AutoMigrate(&User{})
	if err != nil {
		log.Fatal("failed to migrate database", err)
	}
	createDefaultAdmin()
}

func createDefaultAdmin() {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		log.Fatal("failed to generate random password", err)
	}
	randomPassword := hex.EncodeToString(bytes)
	user, err := CreateUser("admin", randomPassword)
	// admin user exists
	if user == nil && err == nil {
		return
	}

	if err != nil {
		log.Fatal("failed to create default admin", err)
	}

	fmt.Printf("=== Default administrator account information ===\n")
	fmt.Printf("Username: admin\n")
	fmt.Printf("Password: %s\n", randomPassword)
	fmt.Printf("========================\n")

	log.Println("Default admin user created successfully")
}

// HashPassword 加密密码
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// checkPassword 验证密码
func checkPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// CreateUser 创建用户（注册）
func CreateUser(username, password string) (*User, error) {
	// 检查用户名是否已存在
	var existingUser User
	if err := db.Where("username = ?", username).First(&existingUser).Error; err == nil {
		if username == "admin" {
			return nil, nil
		}
		return nil, fmt.Errorf("username already exists")
	}

	// 加密密码
	hashedPassword, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %v", err)
	}

	// 创建新用户
	user := User{
		Username: username,
		Password: hashedPassword,
	}

	if err := db.Create(&user).Error; err != nil {
		return nil, fmt.Errorf("failed to create user: %v", err)
	}

	return &user, nil
}

// LoginUser 用户登录
func LoginUser(username, password string) (*User, error) {
	var user User

	// 查找用户
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("invalid user or password")
		}
		return nil, fmt.Errorf("database error: %v", err)
	}

	// 验证密码
	if !checkPassword(password, user.Password) {
		return nil, fmt.Errorf("invalid user or password")
	}

	return &user, nil
}

// GetUserByID 根据ID获取用户
func GetUserByID(id uint) (*User, error) {
	var user User
	if err := db.First(&user, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("database error: %v", err)
	}
	return &user, nil
}

// GetUserByUsername 根据用户名获取用户
func GetUserByUsername(username string) (*User, error) {
	var user User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("database error: %v", err)
	}
	return &user, nil
}

// UpdateUser 更新用户信息
func UpdateUser(id uint, updates map[string]interface{}) (*User, error) {
	var user User
	if err := db.First(&user, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("database error: %v", err)
	}

	// 如果更新密码，需要先加密
	if password, ok := updates["password"].(string); ok {
		hashedPassword, err := HashPassword(password)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %v", err)
		}
		updates["password"] = hashedPassword
	}

	if err := db.Model(&user).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("failed to update user: %v", err)
	}

	return &user, nil
}

// DeleteUser 删除用户
func DeleteUser(id uint) error {
	if err := db.Delete(&User{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete user: %v", err)
	}
	return nil
}

// GetAllUsers 获取所有用户（分页）
func GetAllUsers(page, pageSize int) ([]User, int64, error) {
	var users []User
	var total int64

	// 计算总数
	if err := db.Model(&User{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %v", err)
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := db.Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get users: %v", err)
	}

	return users, total, nil
}
