// Package admin provides an HTTP server for administrative endpoints.
package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/db"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/provider/gemini"
	"github.com/ai8future/airborne/internal/redis"
	"github.com/ai8future/airborne/internal/tenant"
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
	server      *http.Server
	port        int
	grpcAddr    string
	authToken   string
	grpcConn    *grpc.ClientConn
	grpcClient  pb.AirborneServiceClient
	version     VersionInfo
}

// VersionInfo holds version information for the service.
type VersionInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
}

// Config holds admin server configuration.
type Config struct {
	Port        int
	GRPCAddr    string          // Address of the gRPC server (e.g., "localhost:50051")
	AuthToken   string          // Auth token for gRPC calls
	TenantMgr   *tenant.Manager // Tenant manager for accessing API keys
	RedisClient *redis.Client   // Redis client for idempotency
	Version     VersionInfo     // Version information
}

// NewServer creates a new admin HTTP server.
func NewServer(dbClient *db.Client, cfg Config) *Server {
	s := &Server{
		dbClient:    dbClient,
		tenantMgr:   cfg.TenantMgr,
		redisClient: cfg.RedisClient,
		port:        cfg.Port,
		grpcAddr:    cfg.GRPCAddr,
		authToken:   cfg.AuthToken,
		version:     cfg.Version,
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
	mux.HandleFunc("/admin/thread/", corsHandler(s.handleThread))
	mux.HandleFunc("/admin/health", corsHandler(s.handleHealth))
	mux.HandleFunc("/admin/version", corsHandler(s.handleVersion))
	mux.HandleFunc("/admin/test", corsHandler(s.handleTest))
	mux.HandleFunc("/admin/chat", corsHandler(s.handleChat))
	mux.HandleFunc("/admin/upload", corsHandler(s.handleUpload))

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

	var entries []db.ActivityEntry
	var err error

	// Create a base repository for cross-tenant queries
	baseRepo := db.NewRepository(s.dbClient)

	if tenantID != "" {
		entries, err = baseRepo.GetActivityFeedByTenant(ctx, tenantID, limit)
	} else {
		// No tenant specified - get activity from ALL tenants
		entries, err = baseRepo.GetActivityFeedAllTenants(ctx, limit)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.version)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	ID        string  `json:"id,omitempty"`
	Content   string  `json:"content,omitempty"`
	Provider  string  `json:"provider,omitempty"`
	Model     string  `json:"model,omitempty"`
	TokensIn  int     `json:"tokens_in,omitempty"`
	TokensOut int     `json:"tokens_out,omitempty"`
	CostUSD   float64 `json:"cost_usd,omitempty"`
	Cached    bool    `json:"cached,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// handleChat sends a message to an existing thread.
// POST /admin/chat
// Body: {"thread_id": "uuid", "message": "Hello", "tenant_id": "optional", "provider": "gemini"}
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ChatResponse{
			Error: "invalid request body: " + err.Error(),
		})
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
			// Get up to 50 previous messages for context
			dbMessages, msgErr := repo.GetMessages(r.Context(), threadUUID, 50)
			if msgErr == nil && len(dbMessages) > 0 {
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
		RequestId:           threadUUID.String(),    // Use thread_id as request_id for thread continuity
		ConversationHistory: conversationHistory,    // For Gemini/Anthropic (stateless)
		PreviousResponseId:  previousResponseID,     // For OpenAI native continuity
		EnableWebSearch:     true,                   // Enable Google Search grounding by default
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

	// Make gRPC call
	resp, err := client.GenerateReply(ctx, grpcReq)
	if err != nil {
		slog.Error("chat gRPC call failed", "error", err, "thread_id", req.ThreadID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Return 200 with error in body
		json.NewEncoder(w).Encode(ChatResponse{
			Error: "gRPC call failed: " + err.Error(),
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
		ID:        resp.ResponseId,
		Content:   resp.Text,
		Provider:  providerName,
		Model:     resp.Model,
		TokensIn:  inputTokens,
		TokensOut: outputTokens,
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

// buildCompressedHistory creates a compressed conversation history to prevent context window overflow.
// It applies progressive compression: full AI responses for recent messages, truncated for older,
// and drops AI responses entirely for very old conversations.
func buildCompressedHistory(dbMessages []db.Message, previousResponseID *string) []*pb.Message {
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

		content := strings.TrimSpace(msg.Content)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form (max 100MB)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(UploadResponse{
			Error: "failed to parse multipart form: " + err.Error(),
		})
		return
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

	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
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

	// Load conversation history
	var conversationHistory []provider.Message
	if s.dbClient != nil && req.TenantID != "" {
		repo, repoErr := s.dbClient.TenantRepository(req.TenantID)
		if repoErr == nil {
			dbMessages, msgErr := repo.GetMessages(r.Context(), threadUUID, 50)
			if msgErr == nil && len(dbMessages) > 0 {
				for _, msg := range dbMessages {
					conversationHistory = append(conversationHistory, provider.Message{
						Role:    msg.Role,
						Content: msg.Content,
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

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Call Gemini directly
	geminiClient := gemini.NewClient()
	result, err := geminiClient.GenerateReply(ctx, params)
	if err != nil {
		slog.Error("Gemini chat failed", "error", err, "thread_id", req.ThreadID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Return 200 with error in body
		json.NewEncoder(w).Encode(ChatResponse{
			Error: "Gemini call failed: " + err.Error(),
		})
		return
	}

	// Extract token usage
	var inputTokens, outputTokens int
	if result.Usage != nil {
		inputTokens = int(result.Usage.InputTokens)
		outputTokens = int(result.Usage.OutputTokens)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		ID:        result.ResponseID,
		Content:   result.Text,
		Provider:  "gemini",
		Model:     result.Model,
		TokensIn:  inputTokens,
		TokensOut: outputTokens,
	})
}
