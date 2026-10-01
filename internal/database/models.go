package database

import "time"

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type User struct {
	ID               int64     `json:"id"`
	Username         string    `json:"username"`
	Email            string    `json:"email"`
	PasswordHash     string    `json:"-"`
	Role             Role      `json:"role"`
	APIKey           string    `json:"api_key"`
	QuotaBytes       int64     `json:"quota_bytes"`
	StorageUsedBytes int64     `json:"storage_used_bytes"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AuditLog struct {
	ID        int64     `json:"id"`
	UserID    *int64    `json:"user_id,omitempty"`
	Username  string    `json:"username,omitempty"`
	Action    string    `json:"action"`
	IPAddress string    `json:"ip_address"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

type DisplayConfig struct {
	DeviceID              string    `json:"device_id"`
	UserID                int64     `json:"user_id"`
	ZipCode               string    `json:"zip_code"`
	CityName              string    `json:"city_name"`
	Latitude              float64   `json:"latitude"`
	Longitude             float64   `json:"longitude"`
	Timezone              string    `json:"timezone"`
	CalendarURL           string    `json:"calendar_url"`
	FullRefreshMinutes    int       `json:"full_refresh_minutes"`
	PartialRefreshMinutes int       `json:"partial_refresh_minutes"`
	BLEMAC                string    `json:"ble_mac"`
	AutoPush              bool      `json:"auto_push"`
	PushIntervalSeconds   int       `json:"push_interval_seconds"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type CalendarEvent struct {
	ID        int64     `json:"id"`
	DeviceID  string    `json:"device_id"`
	Title     string    `json:"title"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Location  string    `json:"location,omitempty"`
}
