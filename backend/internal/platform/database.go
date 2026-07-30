package platform

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"mego-panel/backend/internal/domain"
)

func OpenDatabase(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	return db, db.AutoMigrate(&domain.Admin{}, &domain.Setting{}, &domain.ServiceState{})
}
