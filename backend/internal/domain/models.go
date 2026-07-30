package domain

import "time"

type Admin struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:80;not null" json:"username"`
	PasswordHash string    `gorm:"not null" json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
type Setting struct {
	Key   string `gorm:"primaryKey;size:120" json:"key"`
	Value string `gorm:"not null" json:"value"`
}
type ServiceState struct {
	Name      string    `gorm:"primaryKey;size:80" json:"name"`
	Installed bool      `json:"installed"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type DashboardStats struct {
	CPUUsage  float64 `json:"cpuUsage"`
	RAMUsage  float64 `json:"ramUsage"`
	DiskUsage float64 `json:"diskUsage"`
	Uptime    string  `json:"uptime"`
	OSVersion string  `json:"osVersion"`
	Hostname  string  `json:"hostname"`
}
