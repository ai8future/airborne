// Package admin provides an HTTP server for administrative endpoints.
package admin

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai8future/chassis-go/v11/guard"
	"github.com/ai8future/chassis-go/v11/health"
	"github.com/ai8future/chassis-go/v11/httpkit"
	"github.com/ai8future/chassis-go/v11/secval"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/db"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/gemini"
	"github.com/ai8future/airborne/internal/redis"
	"github.com/ai8future/airborne/internal/tenant"
	"github.com/ai8future/airborne/internal/validation"
	pricing_db "github.com/ai8future/pricing_db"
	"github.com/google/uuid"
	"google.golang.org/genai"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Server is the HTTP admin server for operational endpoints.
type Server struct {
	dbClient    *db.Client
	tenantMgr   *tenant.Manager
	redisClient *redis.Client
	pricer      *pricing_db.Pricer
	server      *http.Server
	port        int
	grpcAddr    string
	authToken   string
	grpcConn    *grpc.ClientConn
	grpcClient  pb.AirborneServiceClient
	version     VersionInfo
}

const (
	maxAdminUploadBytes       int64 = 100 << 20
	maxAdminUploadMemoryBytes int64 = 8 << 20
)

// VersionInfo holds version information for the service.
type VersionInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
}

// Config holds admin server configuration.
type Config struct {
	Port           int
	GRPCAddr       string                                 // Address of the gRPC server (e.g., "localhost:50051")
	AuthToken      string                                 // Auth token for gRPC calls
	AllowedOrigins []string                               // Browser origins allowed for CORS
	TenantMgr      *tenant.Manager                        // Tenant manager for accessing API keys
	RedisClient    *redis.Client                          // Redis client for idempotency
	Version        VersionInfo                            // Version information
	HealthChecks   map[string]func(context.Context) error // Additional health checks (e.g., RAG dependencies)
}

// NewServer creates a new admin HTTP server.
func NewServer(dbClient *db.Client, cfg Config) *Server {
	// Initialize pricer for cost calculations
	pricer, err := pricing_db.NewPricer()
	if err != nil {
		slog.Warn("failed to initialize pricer, cost calculations will be disabled", "error", err)
	}

	s := &Server{
		dbClient:    dbClient,
		tenantMgr:   cfg.TenantMgr,
		redisClient: cfg.RedisClient,
		pricer:      pricer,
		port:        cfg.Port,
		grpcAddr:    cfg.GRPCAddr,
		authToken:   cfg.AuthToken,
		version:     cfg.Version,
	}

	mux := http.NewServeMux()

	// Register chassis-go health check endpoint with parallel dependency checks
	healthChecks := map[string]health.Check{
		"self": func(_ context.Context) error { return nil },
	}
	if s.dbClient != nil {
		healthChecks["postgres"] = func(ctx context.Context) error {
			return s.dbClient.Ping(ctx)
		}
	}
	if s.redisClient != nil {
		healthChecks["redis"] = func(ctx context.Context) error {
			return s.redisClient.Ping(ctx)
		}
	}
	for name, check := range cfg.HealthChecks {
		healthChecks[name] = check
	}

	// Register endpoints
	mux.HandleFunc("/admin/activity", s.handleActivity)
	mux.HandleFunc("/admin/debug/", s.handleDebug)
	mux.HandleFunc("/admin/thread/", s.handleThread)
	mux.HandleFunc("/admin/health", s.handleHealth)
	mux.Handle("/admin/healthz", health.Handler(healthChecks))
	mux.HandleFunc("/admin/version", s.handleVersion)
	maxBody := guard.MaxBody(2 * 1024 * 1024) // 2 MB for JSON POST endpoints
	mux.Handle("/admin/test", maxBody(http.HandlerFunc(s.handleTest)))
	mux.Handle("/admin/chat", maxBody(http.HandlerFunc(s.handleChat)))
	mux.Handle("/admin/upload", guard.MaxBody(maxAdminUploadBytes)(http.HandlerFunc(s.handleUpload)))

	// Stack chassis-go httpkit middleware: Recovery → CORS → Auth → Tracing → RequestID → Timeout → Logging → routes
	//
	// SECURITY: the HTTP admin server exposes database inspection and write-capable
	// proxy endpoints. It must never rely on "only bind it internally" as the
	// access-control boundary. Health probes stay public; all other /admin routes
	// require the configured admin bearer token before a handler can reach local
	// DB reads or proxy a privileged gRPC request with s.authToken.
	logger := slog.Default()
	handler := httpkit.Recovery(logger)(
		guard.CORS(guard.CORSConfig{
			AllowOrigins: adminAllowedOrigins(cfg.AllowedOrigins),
			AllowMethods: []string{"GET", "POST", "OPTIONS"},
			AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key"},
			MaxAge:       10 * time.Minute,
		})(
			httpkit.Tracing()(
				httpkit.RequestID(
					guard.Timeout(30 * time.Second)(
						httpkit.Logging(logger)(s.requireHTTPAuth(mux)),
					),
				),
			),
		),
	)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 5 * time.Minute, // Must exceed context timeout for LLM requests
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func adminAllowedOrigins(origins []string) []string {
	cleaned := make([]string, 0, len(origins))
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "*" {
			slog.Warn("admin CORS wildcard origin ignored; configure explicit origins")
			continue
		}
		if origin != "" {
			cleaned = append(cleaned, origin)
		}
	}
	if len(cleaned) > 0 {
		return cleaned
	}
	return []string{
		"http://localhost:3000",
		"http://127.0.0.1:3000",
		"http://localhost:4848",
		"http://127.0.0.1:4848",
	}
}

// requireHTTPAuth protects every non-health admin HTTP route with the same
// static admin token used by the gRPC static authenticator. This closes the
// accidental "admin HTTP as unauthenticated privileged proxy" class of bugs.
func (s *Server) requireHTTPAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAdminPath(r.URL.Path) || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		if !constantTimeTokenEqual(extractHTTPAdminToken(r), s.authToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="airborne-admin"`)
			httpkit.JSONError(w, r, http.StatusUnauthorized, "unauthorized")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isPublicAdminPath(path string) bool {
	return path == "/admin/health" || path == "/admin/healthz"
}

func extractHTTPAdminToken(r *http.Request) string {
	if token := normalizeHTTPAuthHeader(r.Header.Get("Authorization")); token != "" {
		return token
	}
	if token := strings.TrimSpace(r.Header.Get("X-API-Key")); token != "" {
		return token
	}
	return ""
}

func normalizeHTTPAuthHeader(value string) string {
	auth := strings.TrimSpace(value)
	if auth == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("bearer "):])
	}
	return auth
}

func constantTimeTokenEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	if len(got) != len(want) {
		// Keep a constant-time operation on attacker-controlled input in the
		// mismatch branch so wrong-length probes do not skip comparison work.
		_ = subtle.ConstantTimeCompare([]byte(got), []byte(got))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
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
		httpkit.JSONError(w, r, http.StatusMethodNotAllowed, "method not allowed")
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

	// Check if database client is available
	if s.dbClient == nil {
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

	// Cross-tenant admin read: a single WithCrossTenant query with tenant_id
	// coming from each row (no per-tenant UNION). An optional tenant_id query
	// param is pushed down as a SQL-side predicate applied before LIMIT, so a
	// sparse tenant still gets up to limit of its own rows ("" = all tenants).
	baseRepo := db.NewRepository(s.dbClient)
	entries, err := baseRepo.GetActivityFeedAllTenants(ctx, tenantID, limit)
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
			"thread_id":          e.ChatID.String(),
			"tenant":             e.TenantID,
			"user_id":            e.UserID,
			"content":            e.Content,
			"full_content":       e.FullContent,
			"provider":           e.Provider,
			"model":              e.ModelID,
			"input_tokens":       e.InputTokens,
			"output_tokens":      e.OutputTokens,
			"tokens_used":        e.TotalTokens,
			"cost_usd":           e.CostUSD,
			"grounding_queries":  e.GroundingQueries,
			"grounding_cost_usd": e.GroundingCostUSD,
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
		httpkit.JSONError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	status := "healthy"
	dbStatus := "not_configured"

	if s.dbClient != nil {
		// Check database connectivity
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// Try ping to verify connectivity
		if err := s.dbClient.Ping(ctx); err != nil {
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

// handleVersion returns version information.
// GET /admin/version
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpkit.JSONError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.version)
}

// handleDebug returns full request/response debug data for a message.
// GET /admin/debug/{message_id}
func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpkit.JSONError(w, r, http.StatusMethodNotAllowed, "method not allowed")
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

	// Check if database client is available
	if s.dbClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "database not configured",
		})
		return
	}

	// Fetch debug data - search across all tenants
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	baseRepo := db.NewRepository(s.dbClient)
	data, err := baseRepo.GetDebugDataAllTenants(ctx, messageID)
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

// handleThread returns the full conversation for a thread.
// GET /admin/thread/{thread_id}
func (s *Server) handleThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpkit.JSONError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract thread ID from path: /admin/thread/{thread_id}
	path := strings.TrimPrefix(r.URL.Path, "/admin/thread/")
	if path == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "thread_id required",
		})
		return
	}

	threadID, err := uuid.Parse(path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "invalid thread_id format",
		})
		return
	}

	// Check if database client is available
	if s.dbClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "database not configured",
		})
		return
	}

	// Fetch thread conversation - search across all tenants
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	baseRepo := db.NewRepository(s.dbClient)
	conv, err := baseRepo.GetThreadConversationAllTenants(ctx, threadID)
	if err != nil {
		slog.Warn("failed to fetch thread conversation", "thread_id", threadID, "error", err)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "thread not found",
			})
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
		}
		return
	}

	// Return conversation data
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conv)
}

// TestRequest is the request body for the test endpoint.
type TestRequest struct {
	Prompt   string `json:"prompt"`
	TenantID string `json:"tenant_id,omitempty"`
	Provider string `json:"provider,omitempty"` // "gemini", "openai", "anthropic"
}

// TestResponse is the response from the test endpoint.
type TestResponse struct {
	Reply        string `json:"reply"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	ProcessingMs int64  `json:"processing_ms"`
	Error        string `json:"error,omitempty"`
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
		httpkit.JSONError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Read and validate request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpkit.JSONError(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	if err := secval.ValidateJSON(body); err != nil {
		httpkit.JSONError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	var req TestRequest
	if err := json.Unmarshal(body, &req); err != nil {
		httpkit.JSONError(w, r, http.StatusBadRequest, "invalid request body: "+err.Error())
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

	// Set timeout (must be less than HTTP WriteTimeout of 120s)
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	start := time.Now()

	// Make gRPC call
	resp, err := client.GenerateReply(ctx, grpcReq)
	if err != nil {
		slog.Error("test gRPC call failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Return 200 with error in body
		json.NewEncoder(w).Encode(TestResponse{
			Error: err.Error(),
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

// ChatRequest is the request body for the chat endpoint.
type ChatRequest struct {
	ThreadID     string `json:"thread_id"`
	Message      string `json:"message"`
	TenantID     string `json:"tenant_id,omitempty"`
	Provider     string `json:"provider,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	FileURI      string `json:"file_uri,omitempty"`       // File URI from /admin/upload
	FileMIMEType string `json:"file_mime_type,omitempty"` // MIME type of the file
	Filename     string `json:"filename,omitempty"`       // Original filename
	RequestID    string `json:"request_id,omitempty"`     // Idempotency key for retry support
}

// ChatResponse is the response from the chat endpoint.
type ChatResponse struct {
	ID               string  `json:"id,omitempty"`
	Content          string  `json:"content,omitempty"`
	Provider         string  `json:"provider,omitempty"`
	Model            string  `json:"model,omitempty"`
	TokensIn         int     `json:"tokens_in,omitempty"`
	TokensOut        int     `json:"tokens_out,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	GroundingQueries int     `json:"grounding_queries,omitempty"`
	GroundingCostUSD float64 `json:"grounding_cost_usd,omitempty"`
	Cached           bool    `json:"cached,omitempty"`
	Error            string  `json:"error,omitempty"`
}

// handleChat sends a message to an existing thread.
// POST /admin/chat
// Body: {"thread_id": "uuid", "message": "Hello", "tenant_id": "optional", "provider": "gemini"}
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpkit.JSONError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Read and validate request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpkit.JSONError(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	if err := secval.ValidateJSON(body); err != nil {
		httpkit.JSONError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		httpkit.JSONError(w, r, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ChatResponse{
			Error: "message is required",
		})
		return
	}

	if strings.TrimSpace(req.ThreadID) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ChatResponse{
			Error: "thread_id is required",
		})
		return
	}

	// Validate thread_id is a valid UUID
	threadUUID, err := uuid.Parse(req.ThreadID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ChatResponse{
			Error: "invalid thread_id format (must be UUID)",
		})
		return
	}

	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID != "" {
		if _, err := validation.ValidateOrGenerateRequestID(req.RequestID); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ChatResponse{
				Error: "invalid request_id format",
			})
			return
		}
	}

	// Idempotency check: if request_id provided, check Redis for duplicate request
	var idempKey string
	if req.RequestID != "" && s.redisClient != nil {
		idempKey = fmt.Sprintf("chat:idem:%s:%s:%s", req.TenantID, req.ThreadID, req.RequestID)
		ctx := r.Context()

		// Try atomic acquire (5 min TTL for processing)
		acquired, acquireErr := s.redisClient.SetNX(ctx, idempKey, "processing", 5*time.Minute)
		if acquireErr != nil {
			slog.Warn("idempotency check failed, proceeding without", "error", acquireErr)
		} else if !acquired {
			// Key exists - check if completed or still processing
			cached, getErr := s.redisClient.Get(ctx, idempKey)
			if getErr == nil && cached != "" && cached != "processing" {
				// Return cached JSON response
				var cachedResp ChatResponse
				if unmarshalErr := json.Unmarshal([]byte(cached), &cachedResp); unmarshalErr == nil {
					cachedResp.Cached = true
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(cachedResp)
					slog.Info("returning cached response", "request_id", req.RequestID, "thread_id", req.ThreadID)
					return
				}
			}
			// Still processing - return 409 Conflict
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(ChatResponse{Error: "Request in progress"})
			return
		}
		// Acquired the key - proceed with processing
		// Set up cleanup on error (via defer that will delete key if response isn't cached)
		defer func() {
			// If idempKey is still set to "processing", delete it to allow retry
			if val, err := s.redisClient.Get(r.Context(), idempKey); err == nil && val == "processing" {
				s.redisClient.Del(r.Context(), idempKey)
			}
		}()
	}

	// If file URI is present, use direct Gemini call (bypasses gRPC)
	if req.FileURI != "" {
		s.handleChatWithFile(w, r, ChatWithFileRequest{
			ThreadID:     req.ThreadID,
			Message:      req.Message,
			TenantID:     req.TenantID,
			Provider:     req.Provider,
			SystemPrompt: req.SystemPrompt,
			FileURI:      req.FileURI,
			FileMIMEType: req.FileMIMEType,
			Filename:     req.Filename,
		})
		return
	}

	// Get gRPC client
	client, err := s.getGRPCClient()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ChatResponse{
			Error: err.Error(),
		})
		return
	}

	// Load and compress conversation history from database if available
	// For Gemini/Anthropic: we need to pass full conversation history (stateless APIs)
	// For OpenAI: we can use PreviousResponseId for native continuity (more efficient)
	// Uses progressive compression to prevent context window overflow
	var conversationHistory []*pb.Message
	var previousResponseID string
	var originalMessageCount int
	if s.dbClient != nil && req.TenantID != "" {
		repo, repoErr := s.dbClient.TenantRepository(req.TenantID)
		if repoErr == nil {
			// Load the chat's active branch (root-first), capped to the most
			// recent messages (the old GetMessages bound). Progressive
			// compression below further caps what is actually forwarded.
			dbMessages, msgErr := repo.GetActiveBranch(r.Context(), threadUUID.String())
			if msgErr == nil && len(dbMessages) > 0 {
				dbMessages = capHistory(dbMessages, maxAdminHistoryMessages)
				originalMessageCount = len(dbMessages)
				conversationHistory = buildCompressedHistory(dbMessages, &previousResponseID)
				slog.Info("loaded conversation history",
					"thread_id", req.ThreadID,
					"original_messages", originalMessageCount,
					"compressed_messages", len(conversationHistory),
					"previous_response_id", previousResponseID)
			}
		}
	}

	// Use system prompt from request, or default
	systemPrompt := req.SystemPrompt
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = "You are a helpful assistant. Continue the conversation naturally."
	}

	// Add context note if there's conversation history
	if len(conversationHistory) > 0 {
		systemPrompt = systemPrompt + "\n\n[Note: Previous conversation messages are provided for context. Focus on the most recent user message.]"
	}

	// Build gRPC request - use thread_id as request_id to continue the thread
	grpcReq := &pb.GenerateReplyRequest{
		Instructions:        systemPrompt,
		UserInput:           req.Message,
		TenantId:            req.TenantID,
		ClientId:            "dashboard-chat",
		RequestId:           threadUUID.String(), // Use thread_id as request_id for thread continuity
		ConversationHistory: conversationHistory, // For Gemini/Anthropic (stateless)
		PreviousResponseId:  previousResponseID,  // For OpenAI native continuity
		EnableWebSearch:     true,                // Enable Google Search grounding by default
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

	// Set timeout (must be less than HTTP WriteTimeout of 120s)
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	// Make gRPC call
	resp, err := client.GenerateReply(ctx, grpcReq)
	if err != nil {
		slog.Error("chat gRPC call failed", "error", err, "thread_id", req.ThreadID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Return 200 with error in body
		json.NewEncoder(w).Encode(ChatResponse{
			Error: err.Error(),
		})
		return
	}

	// Extract token usage
	var inputTokens, outputTokens int
	if resp.Usage != nil {
		inputTokens = int(resp.Usage.InputTokens)
		outputTokens = int(resp.Usage.OutputTokens)
	}

	// Convert provider enum to friendly string
	providerName := strings.ToLower(strings.TrimPrefix(resp.Provider.String(), "PROVIDER_"))

	// Build response
	chatResp := ChatResponse{
		ID:               resp.ResponseId,
		Content:          resp.Text,
		Provider:         providerName,
		Model:            resp.Model,
		TokensIn:         inputTokens,
		TokensOut:        outputTokens,
		GroundingQueries: int(resp.GroundingQueries),
		GroundingCostUSD: resp.GroundingCostUsd,
	}

	// Cache successful response for idempotency (24h TTL)
	if idempKey != "" && s.redisClient != nil {
		if respJSON, err := json.Marshal(chatResp); err == nil {
			if err := s.redisClient.Set(r.Context(), idempKey, string(respJSON), 24*time.Hour); err != nil {
				slog.Warn("failed to cache response for idempotency", "error", err)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chatResp)
}

// maxAdminHistoryMessages caps how much of a chat's active branch the admin
// chat endpoints load as model context, matching the old GetMessages(..., 50)
// bound that predates the relational migration.
const maxAdminHistoryMessages = 50

// capHistory returns the last n messages of a root-first active branch,
// preserving order — i.e. the n most recent turns.
func capHistory(branch []db.ChatMessage, n int) []db.ChatMessage {
	if len(branch) > n {
		return branch[len(branch)-n:]
	}
	return branch
}

// messageContentText extracts the plain-text body from a ChatMessage.Content
// JSONB value. Content is stored as {"text":"..."} (see db.TextContent), so this
// mirrors the SQL COALESCE(content->>'text', content::text, ”) the admin
// read-queries use: an object with a "text" key yields that string, a bare JSON
// string yields its value, and anything else falls back to the raw bytes.
func messageContentText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] == '{' {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err == nil {
			if t, ok := obj["text"]; ok {
				var s string
				if err := json.Unmarshal(t, &s); err == nil {
					return s
				}
			}
		}
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err == nil {
		return s
	}
	return string(trimmed)
}

// buildCompressedHistory creates a compressed conversation history to prevent context window overflow.
// It applies progressive compression: full AI responses for recent messages, truncated for older,
// and drops AI responses entirely for very old conversations.
func buildCompressedHistory(dbMessages []db.ChatMessage, previousResponseID *string) []*pb.Message {
	const (
		maxHistoryChars      = 30000 // ~7,500 tokens, leaves room for response
		maxAIResponseChars   = 500   // Truncate AI responses after fullAIResponsesLimit
		fullAIResponsesLimit = 3     // Include full AI text for first N responses
		dropAIResponsesLimit = 6     // After N responses, only include user messages
	)

	// Count AI responses to determine compression strategy
	aiResponseCount := 0
	for _, msg := range dbMessages {
		if msg.Role == "assistant" {
			aiResponseCount++
		}
	}

	var result []*pb.Message
	totalChars := 0
	currentAIResponse := 0

	for _, msg := range dbMessages {
		// Track previous response ID for OpenAI native continuity
		if msg.Role == "assistant" && msg.ResponseID != nil && *msg.ResponseID != "" {
			*previousResponseID = *msg.ResponseID
			currentAIResponse++
		}

		content := strings.TrimSpace(messageContentText(msg.Content))
		if content == "" {
			continue
		}

		// Handle AI responses based on count - apply progressive compression
		if msg.Role == "assistant" {
			if aiResponseCount > dropAIResponsesLimit {
				// Skip AI responses entirely when there are too many
				continue
			}
			if currentAIResponse > fullAIResponsesLimit && len(content) > maxAIResponseChars {
				// Truncate older AI responses to save tokens
				content = content[:maxAIResponseChars] + "..."
			}
		}

		// Check character limit - stop adding messages when we exceed
		if totalChars+len(content) > maxHistoryChars {
			slog.Debug("history truncated due to char limit", "total_chars", totalChars, "limit", maxHistoryChars)
			break
		}
		totalChars += len(content)

		result = append(result, &pb.Message{
			Role:      msg.Role,
			Content:   content,
			Timestamp: msg.CreatedAt.Unix(),
		})
	}

	return result
}

// UploadResponse is the response from the upload endpoint.
type UploadResponse struct {
	FileURI  string `json:"file_uri,omitempty"`
	Filename string `json:"filename,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Error    string `json:"error,omitempty"`
}

// handleUpload uploads a file to Gemini Files API.
// POST /admin/upload (multipart/form-data)
// Returns the file URI for use in chat.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpkit.JSONError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Defense in depth: MaxBody enforces this at the route wrapper, and
	// MaxBytesReader keeps this handler bounded if it is called directly in
	// tests or future routing changes. ParseMultipartForm's argument is only
	// the memory/disk split, so keep it much lower than the total upload cap.
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminUploadBytes)
	// #nosec G120 -- body is capped above and by route MaxBody; this value is the in-memory threshold.
	if err := r.ParseMultipartForm(maxAdminUploadMemoryBytes); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Error: "failed to parse multipart form: " + err.Error(),
		})
		return
	}
	if r.MultipartForm != nil {
		defer func() {
			if err := r.MultipartForm.RemoveAll(); err != nil {
				slog.Warn("failed to remove multipart temp files", "error", err)
			}
		}()
	}

	// Get the file
	file, header, err := r.FormFile("file")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Error: "file is required",
		})
		return
	}
	defer file.Close()
	if header.Size > maxAdminUploadBytes {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Error: "file exceeds maximum upload size",
		})
		return
	}

	// Get tenant ID
	tenantID := r.FormValue("tenant_id")
	if tenantID == "" {
		tenantID = "email4ai" // Default tenant
	}

	// Get Gemini API key from tenant config
	apiKey, err := s.getGeminiAPIKey(tenantID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Error: err.Error(),
		})
		return
	}

	// Detect MIME type
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = detectMIMEType(header.Filename)
	}

	// Upload to Gemini Files API
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	fileURI, err := s.uploadFileToGemini(ctx, apiKey, file, header.Filename, mimeType)
	if err != nil {
		slog.Error("failed to upload file to Gemini", "error", err, "filename", header.Filename)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(UploadResponse{
			Error: "failed to upload file: " + err.Error(),
		})
		return
	}

	slog.Info("file uploaded to Gemini",
		"filename", header.Filename,
		"mime_type", mimeType,
		"file_uri", fileURI,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UploadResponse{
		FileURI:  fileURI,
		Filename: header.Filename,
		MIMEType: mimeType,
	})
}

// getGeminiAPIKey retrieves the Gemini API key for a tenant.
func (s *Server) getGeminiAPIKey(tenantID string) (string, error) {
	if s.tenantMgr == nil {
		return "", fmt.Errorf("tenant manager not configured")
	}

	tenantCfg, ok := s.tenantMgr.Tenant(tenantID)
	if !ok {
		return "", fmt.Errorf("tenant not found: %s", tenantID)
	}

	providerCfg, ok := tenantCfg.GetProvider("gemini")
	if !ok {
		return "", fmt.Errorf("gemini provider not enabled for tenant: %s", tenantID)
	}

	if providerCfg.APIKey == "" {
		return "", fmt.Errorf("gemini API key not configured for tenant: %s", tenantID)
	}

	return providerCfg.APIKey, nil
}

// uploadFileToGemini uploads a file to Gemini Files API and returns the URI.
func (s *Server) uploadFileToGemini(ctx context.Context, apiKey string, file multipart.File, filename, mimeType string) (string, error) {
	// Create Gemini client
	clientConfig := &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	}

	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return "", fmt.Errorf("create Gemini client: %w", err)
	}

	// Read file content with a hard cap even when this helper is called outside
	// the HTTP handler in tests or future code paths.
	content, err := io.ReadAll(io.LimitReader(file, maxAdminUploadBytes+1))
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	if int64(len(content)) > maxAdminUploadBytes {
		return "", fmt.Errorf("file exceeds maximum upload size of %d bytes", maxAdminUploadBytes)
	}

	// Upload file
	uploadConfig := &genai.UploadFileConfig{
		MIMEType:    mimeType,
		DisplayName: filename,
	}

	uploadedFile, err := client.Files.Upload(ctx, bytes.NewReader(content), uploadConfig)
	if err != nil {
		return "", fmt.Errorf("upload file: %w", err)
	}

	// Wait for file to be processed
	if uploadedFile.State == genai.FileStateProcessing {
		for i := 0; i < 30; i++ { // Max 1 minute wait
			time.Sleep(2 * time.Second)
			uploadedFile, err = client.Files.Get(ctx, uploadedFile.Name, nil)
			if err != nil {
				return "", fmt.Errorf("get file status: %w", err)
			}
			if uploadedFile.State == genai.FileStateActive {
				break
			}
			if uploadedFile.State == genai.FileStateFailed {
				return "", fmt.Errorf("file processing failed")
			}
		}
	}

	return uploadedFile.URI, nil
}

// detectMIMEType guesses MIME type from filename extension.
func detectMIMEType(filename string) string {
	ext := strings.ToLower(filename)
	if idx := strings.LastIndex(ext, "."); idx != -1 {
		ext = ext[idx:]
	}

	mimeTypes := map[string]string{
		".pdf":  "application/pdf",
		".txt":  "text/plain",
		".md":   "text/markdown",
		".csv":  "text/csv",
		".json": "application/json",
		".xml":  "application/xml",
		".html": "text/html",
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
		".svg":  "image/svg+xml",
		".mp3":  "audio/mpeg",
		".wav":  "audio/wav",
		".mp4":  "video/mp4",
		".webm": "video/webm",
		".doc":  "application/msword",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xls":  "application/vnd.ms-excel",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".ppt":  "application/vnd.ms-powerpoint",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}

	if mt, ok := mimeTypes[ext]; ok {
		return mt
	}
	return "application/octet-stream"
}

// ChatWithFileRequest extends ChatRequest with file support.
type ChatWithFileRequest struct {
	ThreadID     string `json:"thread_id"`
	Message      string `json:"message"`
	TenantID     string `json:"tenant_id,omitempty"`
	Provider     string `json:"provider,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	FileURI      string `json:"file_uri,omitempty"`
	FileMIMEType string `json:"file_mime_type,omitempty"`
	Filename     string `json:"filename,omitempty"`
}

// handleChatWithFile handles chat requests with optional file attachments.
// This bypasses gRPC to call the Gemini provider directly when files are present.
func (s *Server) handleChatWithFile(w http.ResponseWriter, r *http.Request, req ChatWithFileRequest) {
	// Validate thread_id is a valid UUID
	threadUUID, err := uuid.Parse(req.ThreadID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ChatResponse{
			Error: "invalid thread_id format (must be UUID)",
		})
		return
	}

	// Get Gemini API key
	apiKey, err := s.getGeminiAPIKey(req.TenantID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ChatResponse{
			Error: err.Error(),
		})
		return
	}

	// Load conversation history (active branch root-first, capped to the most
	// recent messages — the old GetMessages bound).
	var conversationHistory []provider.Message
	if s.dbClient != nil && req.TenantID != "" {
		repo, repoErr := s.dbClient.TenantRepository(req.TenantID)
		if repoErr == nil {
			dbMessages, msgErr := repo.GetActiveBranch(r.Context(), threadUUID.String())
			if msgErr == nil && len(dbMessages) > 0 {
				dbMessages = capHistory(dbMessages, maxAdminHistoryMessages)
				for _, msg := range dbMessages {
					conversationHistory = append(conversationHistory, provider.Message{
						Role:    msg.Role,
						Content: messageContentText(msg.Content),
					})
				}
			}
		}
	}

	// Build system prompt
	systemPrompt := req.SystemPrompt
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = "You are a helpful assistant. Continue the conversation naturally."
	}
	if len(conversationHistory) > 0 {
		systemPrompt = systemPrompt + "\n\n[Note: Previous conversation messages are provided for context. Focus on the most recent user message.]"
	}

	// Build inline images (files)
	var inlineImages []provider.InlineImage
	if req.FileURI != "" {
		inlineImages = append(inlineImages, provider.InlineImage{
			URI:      req.FileURI,
			MIMEType: req.FileMIMEType,
			Filename: req.Filename,
		})
	}

	// Create Gemini provider params
	params := provider.GenerateParams{
		Instructions:        systemPrompt,
		UserInput:           req.Message,
		ConversationHistory: conversationHistory,
		InlineImages:        inlineImages,
		EnableWebSearch:     true,
		Config: provider.ProviderConfig{
			APIKey: apiKey,
			Model:  "gemini-3-pro-preview",
		},
		RequestID: threadUUID.String(),
		ClientID:  "dashboard-chat-file",
	}

	// Add file context to system prompt
	if req.Filename != "" {
		params.FileIDToFilename = map[string]string{
			req.FileURI: req.Filename,
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()

	// Call Gemini directly
	geminiClient := gemini.NewClient()
	result, err := geminiClient.GenerateReply(ctx, params)
	if err != nil {
		slog.Error("Gemini chat failed", "error", err, "thread_id", req.ThreadID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Return 200 with error in body
		json.NewEncoder(w).Encode(ChatResponse{
			Error: err.Error(),
		})
		return
	}

	// Extract token usage
	var inputTokens, outputTokens int
	if result.Usage != nil {
		inputTokens = int(result.Usage.InputTokens)
		outputTokens = int(result.Usage.OutputTokens)
	}

	// Calculate cost using pricing_db
	var costUSD, groundingCostUSD float64
	if s.pricer != nil {
		tokenCost := s.pricer.Calculate(result.Model, int64(inputTokens), int64(outputTokens))
		costUSD = tokenCost.TotalCost
		groundingCostUSD = s.pricer.CalculateGrounding(result.Model, result.GroundingQueries)
	}

	// Generate message ID for the assistant response
	messageID := uuid.New()

	// Persist the whole turn atomically (chat get-or-create by primary key,
	// user message, assistant reply as its child, and the cold debug blob) in a
	// single transaction — the new-model equivalent of the old get-or-create
	// thread + two CreateMessage calls. The failure signal is carried by the
	// message status rather than a content prefix; here the turn succeeded.
	if s.dbClient != nil && req.TenantID != "" {
		repo, repoErr := s.dbClient.TenantRepository(req.TenantID)
		if repoErr == nil {
			providerName := "gemini"
			modelName := result.Model
			totalTokens := inputTokens + outputTokens
			groundingQueries := result.GroundingQueries

			chatID := threadUUID.String()
			userMsg := &db.ChatMessage{
				ID:       uuid.New().String(),
				TenantID: req.TenantID,
				ChatID:   chatID,
				UserID:   "dashboard-user",
				Role:     db.RoleUser,
				Content:  db.TextContent(req.Message),
				Status:   db.ChatMessageStatusComplete,
			}
			assistantMsg := &db.ChatMessage{
				ID:               messageID.String(),
				TenantID:         req.TenantID,
				ChatID:           chatID,
				UserID:           "dashboard-user",
				Role:             db.RoleAssistant,
				Content:          db.TextContent(result.Text),
				ModelID:          &modelName,
				Provider:         &providerName,
				ResponseID:       &result.ResponseID,
				InputTokens:      &inputTokens,
				OutputTokens:     &outputTokens,
				TotalTokens:      &totalTokens,
				CostUSD:          &costUSD,
				GroundingQueries: &groundingQueries,
				GroundingCostUSD: &groundingCostUSD,
				Status:           db.ChatMessageStatusComplete,
			}

			var rawRequestJSON, rawResponseJSON json.RawMessage
			if len(result.RequestJSON) > 0 {
				rawRequestJSON = json.RawMessage(result.RequestJSON)
			}
			if len(result.ResponseJSON) > 0 {
				rawResponseJSON = json.RawMessage(result.ResponseJSON)
			}
			turnDebug := &db.TurnDebug{
				SystemPrompt:    systemPrompt,
				RawRequestJSON:  rawRequestJSON,
				RawResponseJSON: rawResponseJSON,
			}

			if err := repo.PersistTurn(r.Context(), &db.Chat{
				ID:       chatID,
				TenantID: req.TenantID,
				UserID:   "dashboard-user",
				Provider: providerName,
				ModelID:  modelName,
				Status:   db.ChatStatusActive,
			}, userMsg, assistantMsg, turnDebug); err != nil {
				slog.Warn("failed to persist chat with file", "error", err, "thread_id", req.ThreadID)
			} else {
				slog.Info("persisted chat with file",
					"thread_id", req.ThreadID,
					"message_id", messageID,
					"has_request_json", rawRequestJSON != nil,
					"has_response_json", rawResponseJSON != nil,
				)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		ID:               messageID.String(),
		Content:          result.Text,
		Provider:         "gemini",
		Model:            result.Model,
		TokensIn:         inputTokens,
		TokensOut:        outputTokens,
		CostUSD:          costUSD,
		GroundingQueries: result.GroundingQueries,
		GroundingCostUSD: groundingCostUSD,
	})
}
