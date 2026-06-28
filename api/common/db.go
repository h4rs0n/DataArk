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

// ArchiveDocument 保存单个归档 HTML 的可搜索元数据。
// HTML 文件本身仍然以 domain/file_name 的目录结构落盘，这里只保存文件无法表达的外部来源信息。
type ArchiveDocument struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Domain    string    `json:"domain" gorm:"uniqueIndex:idx_archive_documents_identity;not null;size:255"`
	FileName  string    `json:"fileName" gorm:"uniqueIndex:idx_archive_documents_identity;not null;size:1024"`
	SourceURL string    `json:"sourceUrl"`
	Title     string    `json:"title" gorm:"size:1024"`
	Summary   string    `json:"summary" gorm:"type:text"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SearchEvent struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Keyword     string    `json:"keyword" gorm:"index;not null;size:255"`
	ResultCount int       `json:"resultCount" gorm:"not null;default:0"`
	CreatedAt   time.Time `json:"createdAt" gorm:"index"`
}

type ArchiveClickEvent struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Domain    string    `json:"domain" gorm:"index;not null;size:255"`
	FileName  string    `json:"fileName" gorm:"index;not null;size:1024"`
	Path      string    `json:"path" gorm:"index;not null;size:1400"`
	Keyword   string    `json:"keyword" gorm:"size:255"`
	CreatedAt time.Time `json:"createdAt" gorm:"index"`
}

type DiscoverySource struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	Name          string     `json:"name" gorm:"not null;size:255"`
	URL           string     `json:"url" gorm:"uniqueIndex;not null;size:2048"`
	Type          string     `json:"type" gorm:"not null;size:32"`
	Enabled       bool       `json:"enabled" gorm:"not null;default:true"`
	ETag          string     `json:"etag" gorm:"size:1024"`
	LastModified  string     `json:"lastModified" gorm:"size:1024"`
	FailureCount  int        `json:"failureCount" gorm:"not null;default:0"`
	NextFetchAt   *time.Time `json:"nextFetchAt"`
	CrawlConfig   string     `json:"crawlConfig" gorm:"type:text"`
	LastFetchedAt *time.Time `json:"lastFetchedAt"`
	LastError     string     `json:"lastError" gorm:"type:text"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type DiscoveryCandidate struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	SourceID           uint       `json:"sourceId" gorm:"index;not null"`
	SourceName         string     `json:"sourceName" gorm:"size:255"`
	URL                string     `json:"url" gorm:"uniqueIndex;not null;size:2048"`
	CanonicalURL       string     `json:"canonicalUrl" gorm:"size:2048"`
	NormalizedURL      string     `json:"normalizedUrl" gorm:"index;size:2048"`
	Title              string     `json:"title" gorm:"size:1024"`
	Summary            string     `json:"summary" gorm:"type:text"`
	Author             string     `json:"author" gorm:"size:255"`
	BodyText           string     `json:"bodyText" gorm:"type:text"`
	Language           string     `json:"language" gorm:"size:32"`
	WordCount          int        `json:"wordCount" gorm:"not null;default:0"`
	ContentHash        string     `json:"contentHash" gorm:"index;size:128"`
	DedupeKey          string     `json:"dedupeKey" gorm:"index;size:128"`
	DuplicateClusterID string     `json:"duplicateClusterId" gorm:"index;size:128"`
	Topics             string     `json:"topics" gorm:"type:text"`
	Entities           string     `json:"entities" gorm:"type:text"`
	ContentType        string     `json:"contentType" gorm:"index;size:64"`
	ContentStyle       string     `json:"contentStyle" gorm:"index;size:64"`
	QualityScore       float64    `json:"qualityScore" gorm:"not null;default:0"`
	DepthScore         float64    `json:"depthScore" gorm:"not null;default:0"`
	EnrichmentStatus   string     `json:"enrichmentStatus" gorm:"index;size:32"`
	EnrichmentError    string     `json:"enrichmentError" gorm:"type:text"`
	EmbeddingModel     string     `json:"embeddingModel" gorm:"size:255"`
	LLMModel           string     `json:"llmModel" gorm:"size:255"`
	PromptVersion      string     `json:"promptVersion" gorm:"size:64"`
	EnrichedAt         *time.Time `json:"enrichedAt"`
	Status             string     `json:"status" gorm:"index;not null;size:32"`
	Score              float64    `json:"score" gorm:"not null;default:0"`
	PublishedAt        *time.Time `json:"publishedAt"`
	ArchivedTaskID     string     `json:"archivedTaskId" gorm:"size:36"`
	LastSeenAt         time.Time  `json:"lastSeenAt" gorm:"index"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type DiscoveryCandidateFeedback struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	CandidateID uint      `json:"candidateId" gorm:"index;not null"`
	Action      string    `json:"action" gorm:"index;not null;size:32"`
	CreatedAt   time.Time `json:"createdAt" gorm:"index"`
}

type RecommendationSettings struct {
	ID                  uint      `json:"id" gorm:"primaryKey"`
	UserID              uint      `json:"userId" gorm:"uniqueIndex;not null"`
	DailyLimit          int       `json:"dailyLimit" gorm:"not null;default:10"`
	Timezone            string    `json:"timezone" gorm:"not null;size:64"`
	GenerationTime      string    `json:"generationTime" gorm:"not null;size:16"`
	CandidateWindowDays int       `json:"candidateWindowDays" gorm:"not null;default:30"`
	ExplorationRate     float64   `json:"explorationRate" gorm:"not null;default:0.15"`
	Enabled             bool      `json:"enabled" gorm:"not null;default:true"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type RecommendationDay struct {
	ID                 uint                 `json:"id" gorm:"primaryKey"`
	UserID             uint                 `json:"userId" gorm:"uniqueIndex:idx_recommendation_days_user_date;not null"`
	RecommendationDate string               `json:"date" gorm:"uniqueIndex:idx_recommendation_days_user_date;not null;size:10"`
	Timezone           string               `json:"timezone" gorm:"not null;size:64"`
	Status             string               `json:"status" gorm:"index;not null;size:32"`
	RequestedCount     int                  `json:"requestedCount" gorm:"not null;default:10"`
	ActualCount        int                  `json:"actualCount" gorm:"not null;default:0"`
	ProfileVersion     uint                 `json:"profileVersion"`
	LLMModel           string               `json:"llmModel" gorm:"size:255"`
	PromptVersion      string               `json:"promptVersion" gorm:"size:64"`
	GeneratedAt        *time.Time           `json:"generatedAt"`
	Items              []RecommendationItem `json:"items" gorm:"foreignKey:DayID"`
	CreatedAt          time.Time            `json:"createdAt"`
	UpdatedAt          time.Time            `json:"updatedAt"`
}

type RecommendationItem struct {
	ID             uint               `json:"id" gorm:"primaryKey"`
	DayID          uint               `json:"dayId" gorm:"uniqueIndex:idx_recommendation_items_day_candidate;index;not null"`
	UserID         uint               `json:"userId" gorm:"uniqueIndex:idx_recommendation_items_user_candidate;index;not null"`
	CandidateID    uint               `json:"candidateId" gorm:"uniqueIndex:idx_recommendation_items_day_candidate;uniqueIndex:idx_recommendation_items_user_candidate;index;not null"`
	Candidate      DiscoveryCandidate `json:"candidate" gorm:"-"`
	DedupeKey      string             `json:"dedupeKey" gorm:"index;size:128"`
	Rank           int                `json:"rank" gorm:"not null"`
	RetrievalScore float64            `json:"retrievalScore"`
	RerankScore    float64            `json:"rerankScore"`
	FinalScore     float64            `json:"finalScore"`
	Reason         string             `json:"reason" gorm:"type:text"`
	ReasonMetadata string             `json:"reasonMetadata" gorm:"type:text"`
	CreatedAt      time.Time          `json:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

type RecommendationFeedback struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	UserID               uint       `json:"userId" gorm:"index;not null"`
	RecommendationItemID uint       `json:"recommendationItemId" gorm:"index;not null"`
	CandidateID          uint       `json:"candidateId" gorm:"index;not null"`
	Action               string     `json:"action" gorm:"index;not null;size:32"`
	Metadata             string     `json:"metadata" gorm:"type:text"`
	CreatedAt            time.Time  `json:"createdAt" gorm:"index"`
	RevertedAt           *time.Time `json:"revertedAt"`
}

type UserBlockRule struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"userId" gorm:"index;not null"`
	RuleType  string    `json:"type" gorm:"index;not null;size:32"`
	RuleValue string    `json:"value" gorm:"index;not null;size:255"`
	Active    bool      `json:"active" gorm:"index;not null;default:true"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type UserRecommendationProfile struct {
	ID                uint      `json:"id" gorm:"primaryKey"`
	UserID            uint      `json:"userId" gorm:"uniqueIndex;not null"`
	PositiveEmbedding string    `json:"positiveEmbedding" gorm:"type:text"`
	NegativeEmbedding string    `json:"negativeEmbedding" gorm:"type:text"`
	TopicWeights      string    `json:"topicWeights" gorm:"type:text"`
	SourceWeights     string    `json:"sourceWeights" gorm:"type:text"`
	StyleWeights      string    `json:"styleWeights" gorm:"type:text"`
	DepthPreference   float64   `json:"depthPreference"`
	ExplorationRate   float64   `json:"explorationRate"`
	ProfileVersion    uint      `json:"profileVersion" gorm:"not null;default:1"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
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
	err = db.AutoMigrate(
		&User{},
		&ArchiveTask{},
		&ArchiveStat{},
		&ArchiveDocument{},
		&SearchEvent{},
		&ArchiveClickEvent{},
		&DiscoverySource{},
		&DiscoveryCandidate{},
		&DiscoveryCandidateFeedback{},
	)
	if err != nil {
		log.Fatal("failed to migrate database", err)
	}
	if err := RunDatabaseMigrations(db); err != nil {
		log.Fatal("failed to run database migrations", err)
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

func SaveArchiveDocumentMetadata(domain string, fileName string, sourceURL string) error {
	return SaveArchiveDocumentDetails(domain, fileName, sourceURL, "", "")
}

func SaveArchiveDocumentDetails(domain string, fileName string, sourceURL string, title string, summary string) error {
	domain = strings.TrimSpace(domain)
	fileName = strings.TrimSpace(fileName)
	sourceURL = strings.TrimSpace(sourceURL)
	if db == nil || domain == "" || fileName == "" {
		return nil
	}

	document := ArchiveDocument{
		Domain:    domain,
		FileName:  fileName,
		SourceURL: sourceURL,
		Title:     strings.TrimSpace(title),
		Summary:   strings.TrimSpace(summary),
	}
	updates := map[string]interface{}{
		"source_url": sourceURL,
		"updated_at": time.Now(),
	}
	if strings.TrimSpace(title) != "" {
		updates["title"] = strings.TrimSpace(title)
	}
	if strings.TrimSpace(summary) != "" {
		updates["summary"] = strings.TrimSpace(summary)
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "domain"}, {Name: "file_name"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&document).Error
}

func GetArchiveDocument(domain string, fileName string) (*ArchiveDocument, error) {
	domain = strings.TrimSpace(domain)
	fileName = strings.TrimSpace(fileName)
	if db == nil || domain == "" || fileName == "" {
		return nil, gorm.ErrRecordNotFound
	}

	var document ArchiveDocument
	result := db.Where("domain = ? AND file_name = ?", domain, fileName).Limit(1).Find(&document)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &document, nil
}

func GetArchiveDocumentSourceURL(domain string, fileName string) (string, error) {
	domain = strings.TrimSpace(domain)
	fileName = strings.TrimSpace(fileName)
	if db == nil || domain == "" || fileName == "" {
		return "", nil
	}

	var document ArchiveDocument
	result := db.Where("domain = ? AND file_name = ?", domain, fileName).Limit(1).Find(&document)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		return "", nil
	}
	return document.SourceURL, nil
}

func DeleteArchiveDocumentMetadata(domain string, fileName string) error {
	domain = strings.TrimSpace(domain)
	fileName = strings.TrimSpace(fileName)
	if db == nil || domain == "" || fileName == "" {
		return nil
	}

	return db.Where("domain = ? AND file_name = ?", domain, fileName).Delete(&ArchiveDocument{}).Error
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
