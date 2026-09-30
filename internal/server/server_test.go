package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/neonphnx/NeonServices/internal/config"
	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/storage"
)

func setupTestServer(t *testing.T) (*Server, *database.DB, *storage.Manager, func()) {
	tempDir, err := os.MkdirTemp("", "neon_api_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	storagePath := filepath.Join(tempDir, "storage")

	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:            "127.0.0.1",
			Port:            0,
			ReadTimeout:     5 * time.Second,
			WriteTimeout:    5 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Auth: config.AuthConfig{
			JWTSecret:         "test-super-secret-key-32-chars-long!",
			TokenTTL:          1 * time.Hour,
			AllowRegistration: true,
		},
		Database: config.DatabaseConfig{
			Path: dbPath,
		},
		Storage: config.StorageConfig{
			BaseMountPath:     storagePath,
			DefaultQuotaBytes: 10 * 1024 * 1024,
			MaxUploadSizeMB:   5,
		},
	}

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}

	sm := storage.NewManager(storagePath, cfg.Storage.MaxUploadSizeMB)
	_ = sm.EnsureBaseDir()

	srv := New(cfg, db, sm)

	cleanup := func() {
		db.Close()
		os.RemoveAll(tempDir)
	}

	return srv, db, sm, cleanup
}

func TestAPIFlow(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	ts := httptest.NewServer(srv.httpServer.Handler)
	defer ts.Close()

	// 1. Health check
	resp, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("Health check failed: %v, status: %d", err, resp.StatusCode)
	}

	// 2. Register first user (automatically becomes admin)
	regPayload := `{"username":"admin","email":"admin@example.com","password":"secretPassword123!"}`
	resp, err = http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(regPayload))
	if err != nil || resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Registration failed: %v, body: %s", resp.StatusCode, string(body))
	}

	var regRes struct {
		Success bool `json:"success"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&regRes)
	adminToken := regRes.Data.Token
	if adminToken == "" {
		t.Fatalf("Token is empty")
	}

	// 3. Register standard user
	userPayload := `{"username":"bob","email":"bob@example.com","password":"bobPassword123!"}`
	resp, err = http.Post(ts.URL+"/api/v1/auth/register", "application/json", bytes.NewBufferString(userPayload))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("Bob registration failed: %v", resp.StatusCode)
	}
	var bobRes struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&bobRes)
	bobToken := bobRes.Data.Token

	// 4. Test Protected /auth/me for Bob
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("/auth/me failed: %v, status: %d", err, resp.StatusCode)
	}

	// 5. Test File Upload for Bob
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, _ := w.CreateFormFile("file", "notes.txt")
	_, _ = fw.Write([]byte("Hello StationPC PocketCloud Storage!"))
	_ = w.WriteField("path", "projects")
	w.Close()

	uploadReq, _ := http.NewRequest("POST", ts.URL+"/api/v1/storage/upload", &b)
	uploadReq.Header.Set("Authorization", "Bearer "+bobToken)
	uploadReq.Header.Set("Content-Type", w.FormDataContentType())
	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil || uploadResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(uploadResp.Body)
		t.Fatalf("Upload failed: %v, body: %s", uploadResp.StatusCode, string(body))
	}

	// 6. Test File List
	listReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/storage/files?path=projects", nil)
	listReq.Header.Set("Authorization", "Bearer "+bobToken)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil || listResp.StatusCode != http.StatusOK {
		t.Fatalf("List files failed: %v", listResp.StatusCode)
	}

	// 7. Test File Download
	downReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/storage/download?path=projects/notes.txt", nil)
	downReq.Header.Set("Authorization", "Bearer "+bobToken)
	downResp, err := http.DefaultClient.Do(downReq)
	if err != nil || downResp.StatusCode != http.StatusOK {
		t.Fatalf("Download file failed: %v", downResp.StatusCode)
	}
	downData, _ := io.ReadAll(downResp.Body)
	if string(downData) != "Hello StationPC PocketCloud Storage!" {
		t.Errorf("Downloaded content mismatch: %s", string(downData))
	}

	// 8. Admin Endpoint Access Control
	// Bob (non-admin) should receive 403 Forbidden on /api/v1/admin/users
	adminReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/admin/users", nil)
	adminReq.Header.Set("Authorization", "Bearer "+bobToken)
	adminResp, _ := http.DefaultClient.Do(adminReq)
	if adminResp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for non-admin on admin endpoint, got %d", adminResp.StatusCode)
	}

	// Admin user should receive 200 OK
	adminReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminResp2, _ := http.DefaultClient.Do(adminReq)
	if adminResp2.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for admin on admin endpoint, got %d", adminResp2.StatusCode)
	}
}
