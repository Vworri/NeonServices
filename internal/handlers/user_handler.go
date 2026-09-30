package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/neonphnx/NeonServices/internal/auth"
	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/storage"
)

type UserHandler struct {
	db      *database.DB
	storage *storage.Manager
}

func NewUserHandler(db *database.DB, sm *storage.Manager) *UserHandler {
	return &UserHandler{
		db:      db,
		storage: sm,
	}
}

func (h *UserHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil {
		Error(w, http.StatusNotFound, "User not found")
		return
	}

	if usage, err := h.storage.CalculateUserUsage(user.Username); err == nil {
		user.StorageUsedBytes = usage
		_ = h.db.SetStorageUsedExact(user.ID, usage)
	}

	JSON(w, http.StatusOK, user)
}

type UpdateProfileRequest struct {
	Email       string `json:"email,omitempty"`
	OldPassword string `json:"old_password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
}

func (h *UserHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil {
		Error(w, http.StatusNotFound, "User not found")
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if req.Email != "" {
		user.Email = strings.TrimSpace(strings.ToLower(req.Email))
	}

	if req.NewPassword != "" {
		if !auth.CheckPassword(req.OldPassword, user.PasswordHash) {
			Error(w, http.StatusUnauthorized, "Current password does not match")
			return
		}
		if len(req.NewPassword) < 8 {
			Error(w, http.StatusBadRequest, "New password must be at least 8 characters")
			return
		}
		hash, err := auth.HashPassword(req.NewPassword)
		if err != nil {
			Error(w, http.StatusInternalServerError, "Failed to hash password")
			return
		}
		user.PasswordHash = hash
	} else {
		user.PasswordHash = "" // preserve existing
	}

	if err := h.db.UpdateUser(user); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to update profile: "+err.Error())
		return
	}

	updated, _ := h.db.GetUserByID(claims.UserID)
	JSON(w, http.StatusOK, updated)
}

func (h *UserHandler) RegenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	newKey, err := h.db.RegenerateAPIKey(claims.UserID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to regenerate API key: "+err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]string{"api_key": newKey})
}

func (h *UserHandler) ListDevices(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	devices, err := h.db.ListDisplayConfigs(claims.UserID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to fetch devices: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, devices)
}

type SaveDeviceRequest struct {
	DeviceID              string  `json:"device_id"`
	CityName              string  `json:"city_name"`
	Latitude              float64 `json:"latitude"`
	Longitude             float64 `json:"longitude"`
	Timezone              string  `json:"timezone"`
	CalendarURL           string  `json:"calendar_url"`
	FullRefreshMinutes    int     `json:"full_refresh_minutes"`
	PartialRefreshMinutes int     `json:"partial_refresh_minutes"`
}

func (h *UserHandler) SaveDevice(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req SaveDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DeviceID) == "" {
		Error(w, http.StatusBadRequest, "Invalid device payload")
		return
	}

	// Verify device ownership if device already exists
	existing, _ := h.db.GetDisplayConfig(req.DeviceID)
	if existing != nil && existing.UserID != claims.UserID && claims.Role != database.RoleAdmin {
		Error(w, http.StatusForbidden, "Device belongs to another user")
		return
	}

	if req.FullRefreshMinutes <= 0 {
		req.FullRefreshMinutes = 30
	}
	if req.PartialRefreshMinutes <= 0 {
		req.PartialRefreshMinutes = 1
	}

	cfg := &database.DisplayConfig{
		DeviceID:              strings.TrimSpace(req.DeviceID),
		UserID:                claims.UserID,
		CityName:              req.CityName,
		Latitude:              req.Latitude,
		Longitude:             req.Longitude,
		Timezone:              req.Timezone,
		CalendarURL:           req.CalendarURL,
		FullRefreshMinutes:    req.FullRefreshMinutes,
		PartialRefreshMinutes: req.PartialRefreshMinutes,
	}

	if err := h.db.SaveDisplayConfig(cfg); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to save device: "+err.Error())
		return
	}

	JSON(w, http.StatusOK, cfg)
}

func (h *UserHandler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	deviceID := r.PathValue("id")
	if deviceID == "" {
		Error(w, http.StatusBadRequest, "Missing device ID")
		return
	}

	existing, _ := h.db.GetDisplayConfig(deviceID)
	if existing != nil && existing.UserID != claims.UserID && claims.Role != database.RoleAdmin {
		Error(w, http.StatusForbidden, "Device belongs to another user")
		return
	}

	if err := h.db.DeleteDisplayConfig(deviceID); err != nil {
		Error(w, http.StatusInternalServerError, "Failed to delete device: "+err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]string{"status": "deleted", "device_id": deviceID})
}
