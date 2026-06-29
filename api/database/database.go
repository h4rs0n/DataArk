package database

import (
	"DataArk/config"
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB

func InitDB() {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		config.DBHost, config.DBUser, config.DBPassword, config.DBName, config.DBPort)
	openedDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect to the database", err)
	}
	db = openedDB
}

func DB() *gorm.DB {
	return db
}

func SetDB(database *gorm.DB) *gorm.DB {
	oldDB := db
	db = database
	return oldDB
}

func AutoMigrate(models ...interface{}) error {
	if db == nil {
		return nil
	}
	return db.AutoMigrate(models...)
}
