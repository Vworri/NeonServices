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
