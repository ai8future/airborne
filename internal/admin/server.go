// Package admin provides an HTTP server for administrative endpoints.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/db"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Server is the HTTP admin server for operational endpoints.
type Server struct {
	repo       *db.Repository
	server     *http.Server
	port       int
	grpcAddr   string
	authToken  string
	grpcConn   *grpc.ClientConn
	grpcClient pb.AirborneServiceClient
}

// Config holds admin server configuration.
type Config struct {
	Port      int
	GRPCAddr  string // Address of the gRPC server (e.g., "localhost:50051")
	AuthToken string // Auth token for gRPC calls
}

// NewServer creates a new admin HTTP server.
func NewServer(repo *db.Repository, cfg Config) *Server {
	s := &Server{
		repo:      repo,
		port:      cfg.Port,
		grpcAddr:  cfg.GRPCAddr,
		authToken: cfg.AuthToken,
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
	mux.HandleFunc("/admin/debug/", corsHandler(s.handleDebug))
	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))

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
	if s.grpcConn != nil {
		s.grpcConn.Close()
	}
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

// handleDebug returns full request/response debug data for a message.
// GET /admin/debug/{message_id}
func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract message ID from path: /admin/debug/{message_id}
	path := strings.TrimPrefix(r.URL.Path, "/admin/debug/")
	if path == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "message_id required",
		})
		return
	}

	messageID, err := uuid.Parse(path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "invalid message_id format",
		})
		return
	}

	// Check if repository is available
	if s.repo == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "database not configured",
		})
		return
	}

	// Fetch debug data
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	data, err := s.repo.GetDebugData(ctx, messageID)
	if err != nil {
		slog.Warn("failed to fetch debug data", "message_id", messageID, "error", err)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "debug data not found",
			})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
		}
		return
	}

	// Return debug data
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// TestRequest is the request body for the test endpoint.
type TestRequest struct {
	Prompt   string `json:"prompt"`
	TenantID string `json:"tenant_id,omitempty"`
	Provider string `json:"provider,omitempty"` // "gemini", "openai", "anthropic"
}

// TestResponse is the response from the test endpoint.
type TestResponse struct {
	Reply         string `json:"reply"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	InputTokens   int    `json:"input_tokens"`
	OutputTokens  int    `json:"output_tokens"`
	ProcessingMs  int64  `json:"processing_ms"`
	Error         string `json:"error,omitempty"`
}

// getGRPCClient lazily initializes the gRPC client.
func (s *Server) getGRPCClient() (pb.AirborneServiceClient, error) {
	if s.grpcClient != nil {
		return s.grpcClient, nil
	}

	if s.grpcAddr == "" {
		return nil, fmt.Errorf("gRPC address not configured")
	}

	conn, err := grpc.NewClient(s.grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	s.grpcConn = conn
	s.grpcClient = pb.NewAirborneServiceClient(conn)
	return s.grpcClient, nil
}

// handleTest sends a test message to the AI service.
// POST /admin/test
// Body: {"prompt": "Hello", "tenant_id": "optional", "provider": "gemini"}
func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req TestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TestResponse{
			Error: "invalid request body: " + err.Error(),
		})
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TestResponse{
			Error: "prompt is required",
		})
		return
	}

	// Get gRPC client
	client, err := s.getGRPCClient()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(TestResponse{
			Error: err.Error(),
		})
		return
	}

	// Build gRPC request
	grpcReq := &pb.GenerateReplyRequest{
		Instructions: "You are a helpful assistant. Respond concisely.",
		UserInput:    req.Prompt,
		TenantId:     req.TenantID,
		ClientId:     "dashboard-test",
		RequestId:    uuid.New().String(),
	}

	// Set provider if specified
	switch strings.ToLower(req.Provider) {
	case "gemini", "":
		grpcReq.PreferredProvider = pb.Provider_PROVIDER_GEMINI
	case "openai":
		grpcReq.PreferredProvider = pb.Provider_PROVIDER_OPENAI
	case "anthropic":
		grpcReq.PreferredProvider = pb.Provider_PROVIDER_ANTHROPIC
	}

	// Add auth token to context
	ctx := r.Context()
	if s.authToken != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+s.authToken)
	}

	// Set timeout
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	start := time.Now()

	// Make gRPC call
	resp, err := client.GenerateReply(ctx, grpcReq)
	if err != nil {
		slog.Error("test gRPC call failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Return 200 with error in body
		json.NewEncoder(w).Encode(TestResponse{
			Error: "gRPC call failed: " + err.Error(),
		})
		return
	}

	processingMs := time.Since(start).Milliseconds()

	// Extract token usage
	var inputTokens, outputTokens int
	if resp.Usage != nil {
		inputTokens = int(resp.Usage.InputTokens)
		outputTokens = int(resp.Usage.OutputTokens)
	}

	// Convert provider enum to friendly string
	providerName := strings.ToLower(strings.TrimPrefix(resp.Provider.String(), "PROVIDER_"))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TestResponse{
		Reply:        resp.Text,
		Provider:     providerName,
		Model:        resp.Model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		ProcessingMs: processingMs,
	})
}
