package admin

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cliffpyles/aibox/internal/auth"
	"github.com/cliffpyles/aibox/internal/config"
	"github.com/cliffpyles/aibox/internal/tenant"
)

//go:embed frontend/dist/*
var frontendFS embed.FS

// VersionInfo contains build version information
type VersionInfo struct {
	Version   string
	GitCommit string
	BuildTime string
}

// Server is the admin HTTP server
type Server struct {
	mux         *http.ServeMux
	auth        *AdminAuth
	keyStore    *auth.KeyStore
	rateLimiter *auth.RateLimiter
	tenantMgr   *tenant.Manager
	config      *config.Config
	version     VersionInfo
	startTime   time.Time
}

// NewServer creates a new admin HTTP server
func NewServer(
	cfg *config.Config,
	adminAuth *AdminAuth,
	keyStore *auth.KeyStore,
	rateLimiter *auth.RateLimiter,
	tenantMgr *tenant.Manager,
	version VersionInfo,
) *Server {
	s := &Server{
		mux:         http.NewServeMux(),
		auth:        adminAuth,
		keyStore:    keyStore,
		rateLimiter: rateLimiter,
		tenantMgr:   tenantMgr,
		config:      cfg,
		version:     version,
		startTime:   time.Now(),
	}

	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// Auth routes (no auth required)
	s.mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleAuthSetup)
	s.mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)

	// Protected routes
	s.mux.HandleFunc("GET /api/info", s.withAuth(s.handleInfo))
	s.mux.HandleFunc("GET /api/stats", s.withAuth(s.handleStats))

	// API Keys
	s.mux.HandleFunc("GET /api/keys", s.withAuth(s.handleListKeys))
	s.mux.HandleFunc("POST /api/keys", s.withAuth(s.handleCreateKey))
	s.mux.HandleFunc("GET /api/keys/{id}", s.withAuth(s.handleGetKey))
	s.mux.HandleFunc("DELETE /api/keys/{id}", s.withAuth(s.handleDeleteKey))

	// Tenants
	s.mux.HandleFunc("GET /api/tenants", s.withAuth(s.handleListTenants))
	s.mux.HandleFunc("GET /api/tenants/{code}", s.withAuth(s.handleGetTenant))

	// Usage
	s.mux.HandleFunc("GET /api/usage", s.withAuth(s.handleUsage))

	// Providers
	s.mux.HandleFunc("GET /api/providers", s.withAuth(s.handleListProviders))
	s.mux.HandleFunc("GET /api/providers/{name}", s.withAuth(s.handleGetProvider))

	// Serve frontend static files
	s.serveFrontend()
}

func (s *Server) serveFrontend() {
	// Get the embedded frontend files
	subFS, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		slog.Warn("frontend not embedded, admin UI will not work", "error", err)
		s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Admin UI not available. Build frontend first.", http.StatusNotFound)
		})
		return
	}

	fileServer := http.FileServer(http.FS(subFS))

	// Serve static files and handle SPA routing
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Don't serve static files for /api routes
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Check if file exists
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		_, err := fs.Stat(subFS, strings.TrimPrefix(path, "/"))
		if err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	})
}

// ServeHTTP implements http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for development
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.mux.ServeHTTP(w, r)
}

// withAuth is middleware that requires authentication
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := s.extractToken(r)
		if !s.auth.ValidateSession(token) {
			s.jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) extractToken(r *http.Request) string {
	// Try Authorization header first
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Try cookie
	cookie, err := r.Cookie("admin_token")
	if err == nil {
		return cookie.Value
	}

	return ""
}

func (s *Server) jsonResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) jsonError(w http.ResponseWriter, message string, status int) {
	s.jsonResponse(w, map[string]string{"error": message}, status)
}

// Auth handlers

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	token := s.extractToken(r)
	s.jsonResponse(w, map[string]interface{}{
		"password_set":   !s.auth.NeedsSetup(),
		"authenticated":  s.auth.ValidateSession(token),
	}, http.StatusOK)
}

func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if !s.auth.NeedsSetup() {
		s.jsonError(w, "already set up", http.StatusBadRequest)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if err := s.auth.SetPassword(req.Password); err != nil {
		s.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Auto-login after setup
	token, err := s.auth.ValidatePassword(req.Password)
	if err != nil {
		s.jsonError(w, "setup succeeded but login failed", http.StatusInternalServerError)
		return
	}

	s.setAuthCookie(w, token)
	s.jsonResponse(w, map[string]string{"token": token}, http.StatusOK)
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	token, err := s.auth.ValidatePassword(req.Password)
	if err != nil {
		if err == ErrNotSetup {
			s.jsonError(w, "admin password not set up", http.StatusBadRequest)
		} else {
			s.jsonError(w, "invalid password", http.StatusUnauthorized)
		}
		return
	}

	s.setAuthCookie(w, token)
	s.jsonResponse(w, map[string]string{"token": token}, http.StatusOK)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	token := s.extractToken(r)
	if token != "" {
		s.auth.InvalidateSession(token)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	s.jsonResponse(w, map[string]bool{"success": true}, http.StatusOK)
}

func (s *Server) setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_token",
		Value:    token,
		Path:     "/",
		MaxAge:   86400, // 24 hours
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Info handlers

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	s.jsonResponse(w, map[string]interface{}{
		"status":         "healthy",
		"version":        s.version.Version,
		"git_commit":     s.version.GitCommit,
		"build_time":     s.version.BuildTime,
		"uptime_seconds": int(time.Since(s.startTime).Seconds()),
	}, http.StatusOK)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"api_key_count":  0,
		"tenant_count":   0,
		"requests_today": 0,
		"tokens_today":   0,
		"providers":      map[string]string{},
	}

	// Get key count from keystore
	if s.keyStore != nil {
		if keys, err := s.keyStore.ListKeys(r.Context()); err == nil {
			stats["api_key_count"] = len(keys)
		}
	}

	// Get tenant count
	if s.tenantMgr != nil {
		stats["tenant_count"] = s.tenantMgr.TenantCount()
	}

	// Get provider status
	providerStatus := make(map[string]string)
	for name, cfg := range s.config.Providers {
		if cfg.Enabled {
			providerStatus[name] = "configured"
		} else {
			providerStatus[name] = "disabled"
		}
	}
	stats["providers"] = providerStatus

	s.jsonResponse(w, stats, http.StatusOK)
}
