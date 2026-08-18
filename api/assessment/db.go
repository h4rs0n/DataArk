package assessment

import (
	"gorm.io/gorm"
)

var db *gorm.DB

func SetDB(database *gorm.DB) *gorm.DB {
	old := db
	db = database
	return old
}
