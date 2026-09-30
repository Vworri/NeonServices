package handlers

import (
	"net/http"
	"runtime"
	"time"

	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/storage"
)

type AdminHandler struct {
	db        *database.DB
	storage   *storage.Manager
	startTime time.Time
}

func NewAdminHandler(db *database.DB, sm *storage.Manager) *AdminHandler {
	return &AdminHandler{
		db:        db,
		storage:   sm,
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

func (h *AdminHandler) SystemHealth(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	nasStatus, _ := h.storage.GetNASStatus()

	data := map[string]interface{}{
		"status":      "ok",
		"uptime":      time.Since(h.startTime).String(),
		"start_time":  h.startTime.UTC().Format(time.RFC3339),
		"go_version":  runtime.Version(),
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
