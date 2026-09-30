package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/neonphnx/NeonServices/internal/auth"
	"github.com/neonphnx/NeonServices/internal/config"
	"github.com/neonphnx/NeonServices/internal/database"
	"github.com/neonphnx/NeonServices/internal/display"
	"github.com/neonphnx/NeonServices/internal/handlers"
	"github.com/neonphnx/NeonServices/internal/storage"
)

type Server struct {
	cfg        *config.Config
	db         *database.DB
	storageMgr *storage.Manager
	displaySvc *display.Service
	httpServer *http.Server
}

func New(cfg *config.Config, db *database.DB, sm *storage.Manager) *Server {
	s := &Server{
		cfg:        cfg,
		db:         db,
		storageMgr: sm,
		displaySvc: display.NewService(db),
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Wrap root handler with logging, recovery, and CORS
	handler := loggingMiddleware(corsMiddleware(mux))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	return s
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	authHandler := handlers.NewAuthHandler(
		s.db,
		s.storageMgr,
		s.cfg.Auth.JWTSecret,
		s.cfg.Auth.TokenTTL,
		s.cfg.Auth.AllowRegistration,
		s.cfg.Storage.DefaultQuotaBytes,
	)

	storageHandler := handlers.NewStorageHandler(s.db, s.storageMgr)
	adminHandler := handlers.NewAdminHandler(s.db, s.storageMgr, s.cfg)
	userHandler := handlers.NewUserHandler(s.db, s.storageMgr)

	authMiddleware := auth.Middleware(s.cfg.Auth.JWTSecret, s.db)
	adminMiddleware := auth.RequireRole(database.RoleAdmin)

	// Public routes
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		handlers.JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "NeonServices"})
	})

	mux.HandleFunc("POST /api/v1/auth/register", authHandler.Register)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)

	// User self-service authenticated routes
	mux.Handle("GET /api/v1/auth/me", authMiddleware(http.HandlerFunc(authHandler.Me)))
	mux.Handle("GET /api/v1/user/profile", authMiddleware(http.HandlerFunc(userHandler.GetProfile)))
	mux.Handle("PUT /api/v1/user/profile", authMiddleware(http.HandlerFunc(userHandler.UpdateProfile)))
	mux.Handle("POST /api/v1/user/regen-key", authMiddleware(http.HandlerFunc(userHandler.RegenerateAPIKey)))
	mux.Handle("GET /api/v1/user/devices", authMiddleware(http.HandlerFunc(userHandler.ListDevices)))
	mux.Handle("POST /api/v1/user/devices", authMiddleware(http.HandlerFunc(userHandler.SaveDevice)))
	mux.Handle("DELETE /api/v1/user/devices/{id}", authMiddleware(http.HandlerFunc(userHandler.DeleteDevice)))

	// Multi-tenant file storage routes
	mux.Handle("GET /api/v1/storage/status", authMiddleware(http.HandlerFunc(storageHandler.Status)))
	mux.Handle("GET /api/v1/storage/files", authMiddleware(http.HandlerFunc(storageHandler.ListFiles)))
	mux.Handle("POST /api/v1/storage/upload", authMiddleware(http.HandlerFunc(storageHandler.Upload)))
	mux.Handle("GET /api/v1/storage/download", authMiddleware(http.HandlerFunc(storageHandler.Download)))
	mux.Handle("POST /api/v1/storage/mkdir", authMiddleware(http.HandlerFunc(storageHandler.Mkdir)))
	mux.Handle("DELETE /api/v1/storage/delete", authMiddleware(http.HandlerFunc(storageHandler.Delete)))

	// Admin-only routes
	mux.Handle("GET /api/v1/admin/users", authMiddleware(adminMiddleware(http.HandlerFunc(adminHandler.ListUsers))))
	mux.Handle("POST /api/v1/admin/users", authMiddleware(adminMiddleware(http.HandlerFunc(adminHandler.CreateUser))))
	mux.Handle("PUT /api/v1/admin/users/{id}", authMiddleware(adminMiddleware(http.HandlerFunc(adminHandler.UpdateUser))))
	mux.Handle("DELETE /api/v1/admin/users/{id}", authMiddleware(adminMiddleware(http.HandlerFunc(adminHandler.DeleteUser))))
	mux.Handle("POST /api/v1/admin/users/{id}/regen-key", authMiddleware(adminMiddleware(http.HandlerFunc(adminHandler.RegenerateAPIKey))))
	mux.Handle("GET /api/v1/admin/settings", authMiddleware(adminMiddleware(http.HandlerFunc(adminHandler.GetSettings))))
	mux.Handle("POST /api/v1/admin/settings", authMiddleware(adminMiddleware(http.HandlerFunc(adminHandler.UpdateSettings))))
	mux.Handle("GET /api/v1/admin/system", authMiddleware(adminMiddleware(http.HandlerFunc(adminHandler.SystemHealth))))

	// reTerminal E1001 OpenDisplay endpoints
	s.displaySvc.RegisterRoutes(mux)
}

func (s *Server) Start() error {
	// Start background OpenDisplay BLE auto-pusher service
	s.displaySvc.StartAutoPusher(context.Background())
	log.Printf("[NeonServices] API server listening on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server failed: %w", err)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("[NeonServices] Shutting down HTTP server...")
	return s.httpServer.Shutdown(ctx)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[PANIC] %s %s: %v", r.Method, r.URL.Path, rec)
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
			log.Printf("[HTTP] %s %s %d %s", r.Method, r.URL.Path, wrapped.statusCode, time.Since(start))
		}()

		next.ServeHTTP(wrapped, r)
	})
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}
