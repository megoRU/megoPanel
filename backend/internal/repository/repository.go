package repository

import (
	"errors"
	"gorm.io/gorm"
	"mego-panel/backend/internal/domain"
)

type AdminRepository interface {
	Count() (int64, error)
	Create(admin *domain.Admin) error
	ByUsername(username string) (*domain.Admin, error)
}
type SettingRepository interface {
	Get(key string) (string, bool, error)
	Set(key string, value string) error
}
type ServiceRepository interface {
	Get(name string) (*domain.ServiceState, error)
	Save(state *domain.ServiceState) error
}

type GormAdminRepository struct{ db *gorm.DB }

func NewAdminRepository(db *gorm.DB) AdminRepository { return &GormAdminRepository{db: db} }
func (r *GormAdminRepository) Count() (int64, error) {
	var count int64
	return count, r.db.Model(&domain.Admin{}).Count(&count).Error
}
func (r *GormAdminRepository) Create(a *domain.Admin) error { return r.db.Create(a).Error }
func (r *GormAdminRepository) ByUsername(u string) (*domain.Admin, error) {
	var a domain.Admin
	err := r.db.Where("username = ?", u).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &a, err
}

type GormSettingRepository struct{ db *gorm.DB }

func NewSettingRepository(db *gorm.DB) SettingRepository { return &GormSettingRepository{db: db} }
func (r *GormSettingRepository) Get(k string) (string, bool, error) {
	var s domain.Setting
	err := r.db.First(&s, "key = ?", k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return s.Value, true, err
}
func (r *GormSettingRepository) Set(k, v string) error {
	return r.db.Save(&domain.Setting{Key: k, Value: v}).Error
}

type GormServiceRepository struct{ db *gorm.DB }

func NewServiceRepository(db *gorm.DB) ServiceRepository { return &GormServiceRepository{db: db} }
func (r *GormServiceRepository) Get(n string) (*domain.ServiceState, error) {
	var s domain.ServiceState
	err := r.db.First(&s, "name = ?", n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &domain.ServiceState{Name: n, Installed: false}, nil
	}
	return &s, err
}
func (r *GormServiceRepository) Save(s *domain.ServiceState) error { return r.db.Save(s).Error }
