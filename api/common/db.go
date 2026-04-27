package common

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"strings"
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

// ArchiveTask HTML 离线归档任务。
// 这里把任务状态持久化到数据库，而不是只放在内存里，
// 是因为链接离线本身是异步过程，服务重启后仍然需要恢复未完成任务。
type ArchiveTask struct {
	ID             string     `json:"id" gorm:"primaryKey;size:36"`
	URL            string     `json:"url" gorm:"index;not null"`
	Domain         string     `json:"domain" gorm:"not null"`
	Status         string     `json:"status" gorm:"index;not null"`
	FileName       string     `json:"fileName"`
	Error          string     `json:"error" gorm:"type:text"`
	ExternalTaskID string     `json:"externalTaskId"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	StartedAt      *time.Time `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
}

// ArchiveStat HTML 归档统计。
// source 当前对应归档目录下的域名目录，file_count 存储该来源下的 HTML 文件数量。
type ArchiveStat struct {
	Source    string    `json:"source" gorm:"primaryKey;size:255"`
	FileCount int       `json:"fileCount" gorm:"not null;default:0"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ArchiveStatsSnapshot 是接口返回的统计快照，总数由各来源数量求和得到。
type ArchiveStatsSnapshot struct {
	TotalFiles int               `json:"totalFiles"`
	Sources    []ArchiveStatItem `json:"sources"`
}

// ArchiveStatItem 表示单个 URL 来源的 HTML 文件数量。
type ArchiveStatItem struct {
	Source    string `json:"source"`
	FileCount int    `json:"fileCount"`
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

	// 自动迁移数据库表
	err = db.AutoMigrate(&User{}, &ArchiveTask{}, &ArchiveStat{})
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

func CreateArchiveTask(task *ArchiveTask) error {
	return db.Create(task).Error
}

func SaveArchiveTask(task *ArchiveTask) error {
	return db.Save(task).Error
}

func GetArchiveTaskByID(id string) (*ArchiveTask, error) {
	var task ArchiveTask
	if err := db.First(&task, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func GetLatestArchiveTaskByURL(rawURL string) (*ArchiveTask, error) {
	var task ArchiveTask
	if err := db.Where("url = ?", rawURL).Order("created_at desc").First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func FindActiveArchiveTaskByURL(rawURL string) (*ArchiveTask, error) {
	var task ArchiveTask
	if err := db.Where("url = ? AND status IN ?", rawURL, []string{"pending", "running"}).
		Order("created_at desc").
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func ListArchiveTasksByStatuses(statuses []string) ([]ArchiveTask, error) {
	var tasks []ArchiveTask
	if err := db.Where("status IN ?", statuses).Order("created_at asc").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetArchiveStats 读取当前统计快照，并在内存中汇总 HTML 文件总数。
func GetArchiveStats() (*ArchiveStatsSnapshot, error) {
	var stats []ArchiveStat
	if err := db.Order("source asc").Find(&stats).Error; err != nil {
		return nil, err
	}
	return buildArchiveStatsSnapshot(stats), nil
}

// ReplaceArchiveStats 用一份完整快照替换数据库里的归档统计。
func ReplaceArchiveStats(stats []ArchiveStat) (*ArchiveStatsSnapshot, error) {
	if err := db.Transaction(func(tx *gorm.DB) error {
		// 刷新统计以磁盘扫描结果为准，先清空旧快照再写入新快照，避免已删除文件残留在统计中。
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ArchiveStat{}).Error; err != nil {
			return err
		}

		if len(stats) == 0 {
			return nil
		}

		return tx.Create(&stats).Error
	}); err != nil {
		return nil, err
	}

	return buildArchiveStatsSnapshot(stats), nil
}

// IncrementArchiveStat 在新增归档文件后增量更新对应来源的统计数量。
func IncrementArchiveStat(source string, delta int) error {
	source = strings.TrimSpace(source)
	if source == "" || delta == 0 {
		return nil
	}

	stat := ArchiveStat{
		Source:    source,
		FileCount: delta,
	}

	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "source"}},
		// 新来源直接插入，已有来源原子累加，避免并发新增文件时丢失计数。
		DoUpdates: clause.Assignments(map[string]interface{}{
			"file_count": gorm.Expr("archive_stats.file_count + ?", delta),
			"updated_at": time.Now(),
		}),
	}).Create(&stat).Error
}

// DecrementArchiveStat 在删除归档文件后更新缓存统计。
// 统计行不存在时直接忽略，因为用户可能还没有手动刷新过统计快照。
func DecrementArchiveStat(source string, delta int) error {
	source = strings.TrimSpace(source)
	if source == "" || delta <= 0 {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var stat ArchiveStat
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stat, "source = ?", source).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		stat.FileCount -= delta
		if stat.FileCount <= 0 {
			return tx.Delete(&ArchiveStat{Source: source}).Error
		}

		return tx.Model(&stat).Updates(map[string]interface{}{
			"file_count": stat.FileCount,
			"updated_at": time.Now(),
		}).Error
	})
}

func buildArchiveStatsSnapshot(stats []ArchiveStat) *ArchiveStatsSnapshot {
	items := make([]ArchiveStatItem, 0, len(stats))
	totalFiles := 0

	for _, stat := range stats {
		if stat.FileCount < 0 {
			continue
		}
		totalFiles += stat.FileCount
		items = append(items, ArchiveStatItem{
			Source:    stat.Source,
			FileCount: stat.FileCount,
		})
	}

	return &ArchiveStatsSnapshot{
		TotalFiles: totalFiles,
		Sources:    items,
	}
}
