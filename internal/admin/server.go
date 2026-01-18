// Package admin provides an HTTP server for administrative endpoints.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ai8future/airborne/internal/db"
)

// Server is the HTTP admin server for operational endpoints.
type Server struct {
	repo   *db.Repository
	server *http.Server
	port   int
}

// Config holds admin server configuration.
type Config struct {
	Port int
}

// NewServer creates a new admin HTTP server.
func NewServer(repo *db.Repository, cfg Config) *Server {
	s := &Server{
		repo: repo,
		port: cfg.Port,
	}

	mux := http.NewServeMux()

	// CORS middleware wrapper
	corsHandler := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			h(w, r)
		}
	}

	// Register endpoints
	mux.HandleFunc("/admin/activity", corsHandler(s.handleActivity))
	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the admin HTTP server.
func (s *Server) Start() error {
	slog.Info("starting admin HTTP server", "port", s.port)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// handleActivity returns recent activity for the dashboard.
// GET /admin/activity?limit=50&tenant_id=optional
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	limit := 50 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}

	tenantID := r.URL.Query().Get("tenant_id")

	// Check if repository is available
	if s.repo == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"activity": []interface{}{},
			"error":    "database not configured",
		})
		return
	}

	// Fetch activity
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var entries []db.ActivityEntry
	var err error

	if tenantID != "" {
		entries, err = s.repo.GetActivityFeedByTenant(ctx, tenantID, limit)
	} else {
		entries, err = s.repo.GetActivityFeed(ctx, limit)
	}

	if err != nil {
		slog.Error("failed to fetch activity", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Return 200 with error in body (matches Bizops pattern)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"activity": []interface{}{},
			"error":    err.Error(),
		})
		return
	}

	// Convert to response format matching Bizops expectations
	activity := make([]map[string]interface{}, len(entries))
	for i, e := range entries {
		activity[i] = map[string]interface{}{
			"id":                 e.ID.String(),
			"thread_id":          e.ThreadID.String(),
			"tenant":             e.TenantID,
			"user_id":            e.UserID,
			"content":            e.Content,
			"full_content":       e.FullContent,
			"provider":           e.Provider,
			"model":              e.Model,
			"input_tokens":       e.InputTokens,
			"output_tokens":      e.OutputTokens,
			"tokens_used":        e.TotalTokens,
			"cost_usd":           e.CostUSD,
			"thread_cost_usd":    e.ThreadCostUSD,
			"processing_time_ms": e.ProcessingTimeMs,
			"status":             e.Status,
			"timestamp":          e.Timestamp.Format(time.RFC3339),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"activity": activity,
	})
}

// handleHealth returns health status.
// GET /admin/health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := "healthy"
	dbStatus := "not_configured"

	if s.repo != nil {
		// Check database connectivity
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// Try a simple query to verify connectivity
		_, err := s.repo.GetActivityFeed(ctx, 1)
		if err != nil {
			dbStatus = "unhealthy"
			status = "degraded"
		} else {
			dbStatus = "healthy"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   status,
		"database": dbStatus,
	})
}
