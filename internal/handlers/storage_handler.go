package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/neonphnx/NeonServices/internal/auth"
	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/storage"
)

type StorageHandler struct {
	db      *database.DB
	storage *storage.Manager
}

func NewStorageHandler(db *database.DB, sm *storage.Manager) *StorageHandler {
	return &StorageHandler{
		db:      db,
		storage: sm,
	}
}

// Status returns overall NAS status (mount availability, free/used capacity)
func (h *StorageHandler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.storage.GetNASStatus()
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, status)
}

// ListFiles lists directory contents for the authenticated user
func (h *StorageHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	relPath := r.URL.Query().Get("path")
	files, err := h.storage.ListUserFiles(claims.Username, relPath)
	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			Error(w, http.StatusNotFound, "Directory not found")
			return
		}
		if errors.Is(err, storage.ErrPathTraversal) {
			Error(w, http.StatusForbidden, "Path traversal forbidden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"current_path": relPath,
		"items":        files,
	})
}

// Upload handles multipart file upload to the user's NAS directory
func (h *StorageHandler) Upload(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 32MB in-memory buffer before streaming to temp disk
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		Error(w, http.StatusBadRequest, "Failed to parse multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		Error(w, http.StatusBadRequest, "No file provided in 'file' field")
		return
	}
	defer file.Close()

	destPath := r.FormValue("path")
	filename := header.Filename
	if customName := r.FormValue("filename"); customName != "" {
		filename = customName
	}

	// Clean filename
	filename = filepath.Base(filename)
	if filename == "." || filename == "/" || filename == "" {
		Error(w, http.StatusBadRequest, "Invalid filename")
		return
	}

	targetRelPath := filepath.Join(destPath, filename)

	// Check user quota
	user, err := h.db.GetUserByID(claims.UserID)
	if err == nil && user.QuotaBytes > 0 {
		if user.StorageUsedBytes+header.Size > user.QuotaBytes {
			Error(w, http.StatusPaymentRequired, "Storage quota exceeded")
			return
		}
	}

	written, err := h.storage.SaveUserFile(claims.Username, targetRelPath, file)
	if err != nil {
		if errors.Is(err, storage.ErrFileTooLarge) {
			Error(w, http.StatusRequestEntityTooLarge, "File exceeds maximum upload size")
			return
		}
		if errors.Is(err, storage.ErrPathTraversal) {
			Error(w, http.StatusForbidden, "Invalid destination path")
			return
		}
		Error(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save file: %v", err))
		return
	}

	// Update storage used in DB
	_ = h.db.UpdateStorageUsed(claims.UserID, written)

	_ = h.db.LogAudit(&database.AuditLog{
		UserID:    &claims.UserID,
		Username:  claims.Username,
		Action:    "file_uploaded",
		IPAddress: r.RemoteAddr,
		Details:   fmt.Sprintf("Uploaded %s (%d bytes)", targetRelPath, written),
	})

	JSON(w, http.StatusCreated, map[string]interface{}{
		"path": targetRelPath,
		"size": written,
	})
}

// Download serves a file to download or stream (supports HTTP Range headers)
func (h *StorageHandler) Download(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		Error(w, http.StatusBadRequest, "Missing 'path' query parameter")
		return
	}

	file, info, err := h.storage.GetUserFile(claims.Username, relPath)
	if err != nil {
		if errors.Is(err, storage.ErrFileNotFound) {
			Error(w, http.StatusNotFound, "File not found")
			return
		}
		if errors.Is(err, storage.ErrPathTraversal) {
			Error(w, http.StatusForbidden, "Access denied")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer file.Close()

	// If download=true, set Content-Disposition to attachment
	if r.URL.Query().Get("download") == "true" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(relPath)))
	}

	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

type MkdirRequest struct {
	Path string `json:"path"`
}

// Mkdir creates a directory in the user's storage space
func (h *StorageHandler) Mkdir(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req MkdirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		Error(w, http.StatusBadRequest, "Invalid path")
		return
	}

	if err := h.storage.CreateUserDir(claims.Username, req.Path); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusCreated, map[string]string{"status": "created", "path": req.Path})
}

// Delete removes a file or folder
func (h *StorageHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		Error(w, http.StatusBadRequest, "Missing 'path' parameter")
		return
	}

	if err := h.storage.DeleteUserPath(claims.Username, relPath); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Recalculate usage
	if usage, err := h.storage.CalculateUserUsage(claims.Username); err == nil {
		_ = h.db.SetStorageUsedExact(claims.UserID, usage)
	}

	_ = h.db.LogAudit(&database.AuditLog{
		UserID:    &claims.UserID,
		Username:  claims.Username,
		Action:    "file_deleted",
		IPAddress: r.RemoteAddr,
		Details:   fmt.Sprintf("Deleted %s", relPath),
	})

	JSON(w, http.StatusOK, map[string]string{"status": "deleted", "path": relPath})
}
