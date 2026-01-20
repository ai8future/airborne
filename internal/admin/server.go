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
	dbClient   *db.Client
	server     *http.Server
	port       int
	grpcAddr   string
	authToken  string
	grpcConn   *grpc.ClientConn
	grpcClient pb.AirborneServiceClient
	version    VersionInfo
}

// VersionInfo holds version information for the service.
type VersionInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
}

// Config holds admin server configuration.
type Config struct {
	Port      int
	GRPCAddr  string      // Address of the gRPC server (e.g., "localhost:50051")
	AuthToken string      // Auth token for gRPC calls
	Version   VersionInfo // Version information
}

// NewServer creates a new admin HTTP server.
func NewServer(dbClient *db.Client, cfg Config) *Server {
	s := &Server{
		dbClient:  dbClient,
		port:      cfg.Port,
		grpcAddr:  cfg.GRPCAddr,
		authToken: cfg.AuthToken,
		version:   cfg.Version,
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
}

// ChatResponse is the response from the chat endpoint.
type ChatResponse struct {
	ID           string `json:"id,omitempty"`
	Content      string `json:"content,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	TokensIn     int    `json:"tokens_in,omitempty"`
	TokensOut    int    `json:"tokens_out,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Error        string `json:"error,omitempty"`
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		ID:        resp.ResponseId,
		Content:   resp.Text,
		Provider:  providerName,
		Model:     resp.Model,
		TokensIn:  inputTokens,
		TokensOut: outputTokens,
	})
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
