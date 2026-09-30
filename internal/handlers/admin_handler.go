package handlers

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/neonphnx/NeonServices/internal/auth"
	"github.com/neonphnx/NeonServices/internal/config"
	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/storage"
)

type AdminHandler struct {
	db        *database.DB
	storage   *storage.Manager
	cfg       *config.Config
	startTime time.Time
}

func NewAdminHandler(db *database.DB, sm *storage.Manager, cfg *config.Config) *AdminHandler {
	return &AdminHandler{
		db:        db,
		storage:   sm,
		cfg:       cfg,
		startTime: time.Now(),
	}
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.db.ListUsers()
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to fetch users: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, users)
}

type AdminCreateUserRequest struct {
	Username   string        `json:"username"`
	Email      string        `json:"email"`
	Password   string        `json:"password"`
	APIKey     string        `json:"api_key,omitempty"`
	Role       database.Role `json:"role"`
	QuotaBytes int64         `json:"quota_bytes"`
}

func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req AdminCreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if len(req.Username) < 3 {
		Error(w, http.StatusBadRequest, "Username must be at least 3 characters")
		return
	}
	if len(req.Password) < 8 {
		Error(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}
	if req.Role != database.RoleAdmin && req.Role != database.RoleUser {
		req.Role = database.RoleUser
	}
	if req.QuotaBytes <= 0 {
		req.QuotaBytes = h.cfg.Storage.DefaultQuotaBytes
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		apiKey = database.GenerateAPIKey()
	}

	user := &database.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hash,
		Role:         req.Role,
		APIKey:       apiKey,
		QuotaBytes:   req.QuotaBytes,
	}

	if err := h.db.CreateUser(user); err != nil {
		Error(w, http.StatusConflict, "Username or email is already taken")
		return
	}

	// Prepare NAS directory
	_, _ = h.storage.GetUserRootDir(user.Username)

	_ = h.db.LogAudit(&database.AuditLog{
		Action:    "admin_create_user",
		IPAddress: r.RemoteAddr,
		Details:   "Admin created user " + user.Username,
	})

	JSON(w, http.StatusCreated, user)
}

type AdminUpdateUserRequest struct {
	Username   string        `json:"username"`
	Email      string        `json:"email"`
	Password   string        `json:"password,omitempty"`
	APIKey     string        `json:"api_key,omitempty"`
	Role       database.Role `json:"role"`
	QuotaBytes int64         `json:"quota_bytes"`
}

func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := h.db.GetUserByID(id)
	if err != nil {
		Error(w, http.StatusNotFound, "User not found")
		return
	}

	var req AdminUpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Username != "" {
		user.Username = strings.TrimSpace(req.Username)
	}
	if req.Email != "" {
		user.Email = strings.TrimSpace(strings.ToLower(req.Email))
	}
	if req.Role == database.RoleAdmin || req.Role == database.RoleUser {
		user.Role = req.Role
	}
	if req.QuotaBytes > 0 {
		user.QuotaBytes = req.QuotaBytes
	}
	if req.APIKey != "" {
		user.APIKey = strings.TrimSpace(req.APIKey)
	}
	if req.Password != "" {
		hash, err := auth.HashPassword(req.Password)
		if err == nil {
			user.PasswordHash = hash
		}
	} else {
		user.PasswordHash = "" // don't overwrite if empty
	}

	if err := h.db.UpdateUser(user); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update user: "+err.Error())
		return
	}

	_ = h.db.LogAudit(&database.AuditLog{
		Action:    "admin_update_user",
		IPAddress: r.RemoteAddr,
		Details:   "Admin updated user " + user.Username,
	})

	updated, _ := h.db.GetUserByID(id)
	JSON(w, http.StatusOK, updated)
}

func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := h.db.GetUserByID(id)
	if err != nil {
		Error(w, http.StatusNotFound, "User not found")
		return
	}

	if err := h.db.DeleteUser(id); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to delete user: "+err.Error())
		return
	}

	// Delete user storage folder
	_ = h.storage.DeleteUserPath(user.Username, "")

	_ = h.db.LogAudit(&database.AuditLog{
		Action:    "admin_delete_user",
		IPAddress: r.RemoteAddr,
		Details:   "Admin deleted user " + user.Username,
	})

	JSON(w, http.StatusOK, map[string]string{"status": "deleted", "username": user.Username})
}

func (h *AdminHandler) RegenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	newKey, err := h.db.RegenerateAPIKey(id)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to regenerate API key: "+err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]string{"api_key": newKey})
}

func (h *AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, h.cfg)
}

type UpdateSettingsRequest struct {
	Port              int    `json:"port"`
	AllowRegistration bool   `json:"allow_registration"`
	BaseMountPath     string `json:"base_mount_path"`
	DefaultQuotaGB    int64  `json:"default_quota_gb"`
	MaxUploadSizeMB   int64  `json:"max_upload_size_mb"`
}

func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req UpdateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid payload")
		return
	}

	if req.Port > 0 {
		h.cfg.Server.Port = req.Port
	}
	h.cfg.Auth.AllowRegistration = req.AllowRegistration
	if req.BaseMountPath != "" {
		h.cfg.Storage.BaseMountPath = req.BaseMountPath
	}
	if req.DefaultQuotaGB > 0 {
		h.cfg.Storage.DefaultQuotaBytes = req.DefaultQuotaGB * 1024 * 1024 * 1024
	}
	if req.MaxUploadSizeMB > 0 {
		h.cfg.Storage.MaxUploadSizeMB = req.MaxUploadSizeMB
	}

	// Persist to config.yaml if possible
	_ = h.cfg.Save("config.yaml")

	_ = h.db.LogAudit(&database.AuditLog{
		Action:    "admin_update_settings",
		IPAddress: r.RemoteAddr,
		Details:   "Admin updated system-wide settings",
	})

	JSON(w, http.StatusOK, h.cfg)
}

func (h *AdminHandler) SystemHealth(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	nasStatus, _ := h.storage.GetNASStatus()

	data := map[string]interface{}{
		"status":        "ok",
		"uptime":        time.Since(h.startTime).String(),
		"start_time":    h.startTime.UTC().Format(time.RFC3339),
		"go_version":    runtime.Version(),
		"num_goroutine": runtime.NumGoroutine(),
		"memory": map[string]interface{}{
			"alloc_mb":       m.Alloc / 1024 / 1024,
			"total_alloc_mb": m.TotalAlloc / 1024 / 1024,
			"sys_mb":         m.Sys / 1024 / 1024,
			"num_gc":         m.NumGC,
		},
		"nas_storage": nasStatus,
	}

	JSON(w, http.StatusOK, data)
}
