package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/neonphnx/NeonServices/internal/auth"
	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/storage"
)

type AuthHandler struct {
	db             *database.DB
	storage        *storage.Manager
	jwtSecret      string
	tokenTTL       time.Duration
	allowRegister  bool
	defaultQuota   int64
}

func NewAuthHandler(db *database.DB, sm *storage.Manager, jwtSecret string, tokenTTL time.Duration, allowRegister bool, defaultQuota int64) *AuthHandler {
	return &AuthHandler{
		db:            db,
		storage:       sm,
		jwtSecret:     jwtSecret,
		tokenTTL:      tokenTTL,
		allowRegister: allowRegister,
		defaultQuota:  defaultQuota,
	}
}

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string         `json:"token"`
	User  *database.User `json:"user"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.allowRegister {
		// Check if any user exists; if none exist, allow creating initial admin
		count, err := h.db.CountUsers()
		if err != nil || count > 0 {
			Error(w, http.StatusForbidden, "Public user registration is disabled")
			return
		}
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if len(req.Username) < 3 || len(req.Username) > 32 {
		Error(w, http.StatusBadRequest, "Username must be between 3 and 32 characters")
		return
	}
	if len(req.Password) < 8 {
		Error(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}
	if !strings.Contains(req.Email, "@") {
		Error(w, http.StatusBadRequest, "Invalid email address")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	role := database.RoleUser
	// If first user in system, make them admin
	userCount, _ := h.db.CountUsers()
	if userCount == 0 {
		role = database.RoleAdmin
	}

	user := &database.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hash,
		Role:         role,
		QuotaBytes:   h.defaultQuota,
	}

	if err := h.db.CreateUser(user); err != nil {
		Error(w, http.StatusConflict, "Username or email is already registered")
		return
	}

	// Prepare user directory on NAS storage
	_, _ = h.storage.GetUserRootDir(user.Username)

	token, err := auth.GenerateToken(user.ID, user.Username, user.Role, h.jwtSecret, h.tokenTTL)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	_ = h.db.LogAudit(&database.AuditLog{
		UserID:    &user.ID,
		Username:  user.Username,
		Action:    "user_registered",
		IPAddress: r.RemoteAddr,
		Details:   "Account created",
	})

	JSON(w, http.StatusCreated, AuthResponse{
		Token: token,
		User:  user,
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	user, err := h.db.GetUserByUsername(strings.TrimSpace(req.Username))
	if err != nil {
		Error(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		Error(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username, user.Role, h.jwtSecret, h.tokenTTL)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	_ = h.db.LogAudit(&database.AuditLog{
		UserID:    &user.ID,
		Username:  user.Username,
		Action:    "user_login",
		IPAddress: r.RemoteAddr,
		Details:   "Successful authentication",
	})

	JSON(w, http.StatusOK, AuthResponse{
		Token: token,
		User:  user,
	})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	user, err := h.db.GetUserByID(claims.UserID)
	if err != nil {
		Error(w, http.StatusNotFound, "User record not found")
		return
	}

	// Update user storage usage directly from NAS storage
	if usage, err := h.storage.CalculateUserUsage(user.Username); err == nil {
		user.StorageUsedBytes = usage
		_ = h.db.SetStorageUsedExact(user.ID, usage)
	}

	JSON(w, http.StatusOK, user)
}
