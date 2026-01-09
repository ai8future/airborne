package service

import (
	"context"
	"strings"
	"testing"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/auth"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/rag"
	"github.com/ai8future/airborne/internal/rag/testutil"
	"github.com/ai8future/airborne/internal/rag/vectorstore"
	"github.com/ai8future/airborne/internal/tenant"
	"github.com/ai8future/airborne/internal/validation"
)

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	name             string
	generateResult   provider.GenerateResult
	generateErr      error
	supportsFile     bool
	supportsWeb      bool
	supportsNative   bool
	supportsStream   bool
	generateCalls    []provider.GenerateParams
	streamCalls      []provider.GenerateParams
}

func newMockProvider(name string) *mockProvider {
	return &mockProvider{
		name:          name,
		supportsFile:  true,
		supportsWeb:   true,
		supportsStream: true,
		generateResult: provider.GenerateResult{
			Text:       "Mock response",
			ResponseID: "resp-123",
			Model:      "mock-model",
			Usage: &provider.Usage{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		},
	}
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	m.generateCalls = append(m.generateCalls, params)
	return m.generateResult, m.generateErr
}

func (m *mockProvider) GenerateReplyStream(ctx context.Context, params provider.GenerateParams) (<-chan provider.StreamChunk, error) {
	m.streamCalls = append(m.streamCalls, params)
	if m.generateErr != nil {
		return nil, m.generateErr
	}
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{
		Type:       provider.ChunkTypeComplete,
		ResponseID: "resp-stream-123",
		Model:      "mock-model",
	}
	close(ch)
	return ch, nil
}

func (m *mockProvider) SupportsFileSearch() bool       { return m.supportsFile }
func (m *mockProvider) SupportsWebSearch() bool        { return m.supportsWeb }
func (m *mockProvider) SupportsNativeContinuity() bool { return m.supportsNative }
func (m *mockProvider) SupportsStreaming() bool        { return m.supportsStream }

// ctxWithChatPermission creates a context with chat permission for testing.
func ctxWithChatPermissionAndTenant(clientID string, tenantCfg *tenant.TenantConfig) context.Context {
	ctx := context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    clientID,
		Permissions: []auth.Permission{auth.PermissionChat, auth.PermissionChatStream},
	})
	if tenantCfg != nil {
		ctx = context.WithValue(ctx, auth.TenantContextKey, tenantCfg)
	}
	return ctx
}

// ctxWithAdminAndChatPermission creates a context with both admin and chat permissions.
func ctxWithAdminAndChatPermission(clientID string, tenantCfg *tenant.TenantConfig) context.Context {
	ctx := context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    clientID,
		Permissions: []auth.Permission{auth.PermissionChat, auth.PermissionChatStream, auth.PermissionAdmin},
	})
	if tenantCfg != nil {
		ctx = context.WithValue(ctx, auth.TenantContextKey, tenantCfg)
	}
	return ctx
}

// createTestTenantConfig creates a test tenant configuration.
func createTestTenantConfig(providers ...string) *tenant.TenantConfig {
	cfg := &tenant.TenantConfig{
		TenantID:    "test-tenant",
		DisplayName: "Test Tenant",
		Providers:   make(map[string]tenant.ProviderConfig),
	}
	for _, p := range providers {
		cfg.Providers[p] = tenant.ProviderConfig{
			Enabled: true,
			APIKey:  "test-key-" + p,
			Model:   "test-model-" + p,
		}
	}
	return cfg
}

// createChatServiceWithMocks creates a ChatService with mock providers for testing.
func createChatServiceWithMocks(mockOpenAI, mockGemini, mockAnthropic *mockProvider, ragService *rag.Service) *ChatService {
	return &ChatService{
		openaiProvider:    mockOpenAI,
		geminiProvider:    mockGemini,
		anthropicProvider: mockAnthropic,
		ragService:        ragService,
	}
}

// ==================== hasCustomBaseURL Tests ====================

func TestHasCustomBaseURL_NoConfigs(t *testing.T) {
	req := &pb.GenerateReplyRequest{
		UserInput: "test",
	}
	if hasCustomBaseURL(req) {
		t.Error("expected false when no provider configs")
	}
}

func TestHasCustomBaseURL_EmptyBaseURL(t *testing.T) {
	req := &pb.GenerateReplyRequest{
		UserInput: "test",
		ProviderConfigs: map[string]*pb.ProviderConfig{
			"openai": {Model: "gpt-4"},
		},
	}
	if hasCustomBaseURL(req) {
		t.Error("expected false when base_url is empty")
	}
}

func TestHasCustomBaseURL_WithBaseURL(t *testing.T) {
	req := &pb.GenerateReplyRequest{
		UserInput: "test",
		ProviderConfigs: map[string]*pb.ProviderConfig{
			"openai": {BaseUrl: "https://custom.example.com"},
		},
	}
	if !hasCustomBaseURL(req) {
		t.Error("expected true when base_url is set")
	}
}

func TestHasCustomBaseURL_WhitespaceOnly(t *testing.T) {
	req := &pb.GenerateReplyRequest{
		UserInput: "test",
		ProviderConfigs: map[string]*pb.ProviderConfig{
			"openai": {BaseUrl: "   "},
		},
	}
	if hasCustomBaseURL(req) {
		t.Error("expected false when base_url is whitespace only")
	}
}

func TestHasCustomBaseURL_MultipleConfigs(t *testing.T) {
	req := &pb.GenerateReplyRequest{
		UserInput: "test",
		ProviderConfigs: map[string]*pb.ProviderConfig{
			"openai":  {Model: "gpt-4"},
			"gemini":  {BaseUrl: "https://custom.gemini.com"},
			"anthropic": {Model: "claude-3"},
		},
	}
	if !hasCustomBaseURL(req) {
		t.Error("expected true when any provider has base_url")
	}
}

// ==================== formatRAGContext Tests ====================

func TestFormatRAGContext_Empty(t *testing.T) {
	result := formatRAGContext(nil)
	if result != "" {
		t.Errorf("expected empty string for nil chunks, got %q", result)
	}

	result = formatRAGContext([]rag.RetrieveResult{})
	if result != "" {
		t.Errorf("expected empty string for empty chunks, got %q", result)
	}
}

func TestFormatRAGContext_SingleChunk(t *testing.T) {
	chunks := []rag.RetrieveResult{
		{Text: "This is chunk text.", Filename: "doc.pdf", ChunkIndex: 0, Score: 0.95},
	}
	result := formatRAGContext(chunks)

	if !strings.Contains(result, "doc.pdf") {
		t.Error("expected result to contain filename")
	}
	if !strings.Contains(result, "This is chunk text.") {
		t.Error("expected result to contain chunk text")
	}
	if !strings.Contains(result, "[1]") {
		t.Error("expected result to contain numbered index")
	}
	if !strings.Contains(result, "Relevant context from uploaded documents") {
		t.Error("expected result to contain header")
	}
}

func TestFormatRAGContext_MultipleChunks(t *testing.T) {
	chunks := []rag.RetrieveResult{
		{Text: "First chunk.", Filename: "doc1.pdf", ChunkIndex: 0, Score: 0.95},
		{Text: "Second chunk.", Filename: "doc2.pdf", ChunkIndex: 1, Score: 0.85},
		{Text: "Third chunk.", Filename: "doc3.pdf", ChunkIndex: 0, Score: 0.75},
	}
	result := formatRAGContext(chunks)

	if !strings.Contains(result, "[1]") {
		t.Error("expected [1] marker")
	}
	if !strings.Contains(result, "[2]") {
		t.Error("expected [2] marker")
	}
	if !strings.Contains(result, "[3]") {
		t.Error("expected [3] marker")
	}
	if !strings.Contains(result, "doc1.pdf") {
		t.Error("expected doc1.pdf")
	}
	if !strings.Contains(result, "doc2.pdf") {
		t.Error("expected doc2.pdf")
	}
	if !strings.Contains(result, "doc3.pdf") {
		t.Error("expected doc3.pdf")
	}
}

// ==================== ragChunksToCitations Tests ====================

func TestRagChunksToCitations_Empty(t *testing.T) {
	result := ragChunksToCitations(nil)
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d citations", len(result))
	}
}

func TestRagChunksToCitations_Basic(t *testing.T) {
	chunks := []rag.RetrieveResult{
		{Text: "Short text", Filename: "doc.pdf", ChunkIndex: 0, Score: 0.95},
	}
	result := ragChunksToCitations(chunks)

	if len(result) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(result))
	}
	if result[0].Type != provider.CitationTypeFile {
		t.Errorf("expected CitationTypeFile, got %v", result[0].Type)
	}
	if result[0].Provider != "qdrant" {
		t.Errorf("expected provider 'qdrant', got %s", result[0].Provider)
	}
	if result[0].Filename != "doc.pdf" {
		t.Errorf("expected filename 'doc.pdf', got %s", result[0].Filename)
	}
	if result[0].Snippet != "Short text" {
		t.Errorf("expected snippet 'Short text', got %s", result[0].Snippet)
	}
}

func TestRagChunksToCitations_TruncatesLongText(t *testing.T) {
	longText := strings.Repeat("a", 300) // 300 characters
	chunks := []rag.RetrieveResult{
		{Text: longText, Filename: "doc.pdf", ChunkIndex: 0, Score: 0.95},
	}
	result := ragChunksToCitations(chunks)

	if len(result) != 1 {
		t.Fatalf("expected 1 citation, got %d", len(result))
	}
	if len(result[0].Snippet) != 203 { // 200 + "..."
		t.Errorf("expected snippet length 203, got %d", len(result[0].Snippet))
	}
	if !strings.HasSuffix(result[0].Snippet, "...") {
		t.Error("expected snippet to end with ...")
	}
}

// ==================== prepareRequest Tests ====================

func TestPrepareRequest_EmptyUserInput(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput: "",
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for empty user_input")
	}
	if !strings.Contains(err.Error(), "user_input") {
		t.Errorf("expected error about user_input, got: %v", err)
	}
}

func TestPrepareRequest_WhitespaceOnlyUserInput(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput: "   \t\n  ",
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for whitespace-only user_input")
	}
}

func TestPrepareRequest_UserInputTooLarge(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	largeInput := strings.Repeat("a", validation.MaxUserInputBytes+1)
	req := &pb.GenerateReplyRequest{
		UserInput: largeInput,
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for oversized user_input")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected error about size, got: %v", err)
	}
}

func TestPrepareRequest_InstructionsTooLarge(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	largeInstructions := strings.Repeat("b", validation.MaxInstructionsBytes+1)
	req := &pb.GenerateReplyRequest{
		UserInput:    "Hello",
		Instructions: largeInstructions,
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for oversized instructions")
	}
	if !strings.Contains(err.Error(), "instructions") {
		t.Errorf("expected error about instructions, got: %v", err)
	}
}

func TestPrepareRequest_HistoryTooLong(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	history := make([]*pb.Message, validation.MaxHistoryCount+1)
	for i := range history {
		history[i] = &pb.Message{Role: "user", Content: "msg"}
	}

	req := &pb.GenerateReplyRequest{
		UserInput:           "Hello",
		ConversationHistory: history,
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for oversized history")
	}
	if !strings.Contains(err.Error(), "history") {
		t.Errorf("expected error about history, got: %v", err)
	}
}

func TestPrepareRequest_MetadataTooLarge(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	metadata := make(map[string]string)
	for i := 0; i < validation.MaxMetadataEntries+1; i++ {
		metadata[string(rune('a'+i%26))+string(rune('0'+i))] = "value"
	}

	req := &pb.GenerateReplyRequest{
		UserInput: "Hello",
		Metadata:  metadata,
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for oversized metadata")
	}
	if !strings.Contains(err.Error(), "metadata") {
		t.Errorf("expected error about metadata, got: %v", err)
	}
}

func TestPrepareRequest_InvalidRequestID(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput: "Hello",
		RequestId: "invalid!@#$%",
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for invalid request_id")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected error about invalid request_id, got: %v", err)
	}
}

func TestPrepareRequest_CustomBaseURLRequiresAdmin(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")

	// Test without admin permission
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)
	req := &pb.GenerateReplyRequest{
		UserInput: "Hello",
		ProviderConfigs: map[string]*pb.ProviderConfig{
			"openai": {BaseUrl: "https://custom.example.com"},
		},
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected permission error for custom base_url without admin")
	}
	if !strings.Contains(err.Error(), "admin") {
		t.Errorf("expected error about admin permission, got: %v", err)
	}

	// Test with admin permission - should succeed
	ctxAdmin := ctxWithAdminAndChatPermission("admin-client", tenantCfg)
	prepared, err := svc.prepareRequest(ctxAdmin, req)
	if err != nil {
		t.Fatalf("expected success with admin permission, got: %v", err)
	}
	if prepared == nil {
		t.Fatal("expected prepared request")
	}
}

func TestPrepareRequest_ProviderSelectionOpenAI(t *testing.T) {
	mockOpenAI := newMockProvider("openai")
	svc := createChatServiceWithMocks(mockOpenAI, newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "Hello",
		PreferredProvider: pb.Provider_PROVIDER_OPENAI,
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}
	if prepared.provider.Name() != "openai" {
		t.Errorf("expected openai provider, got %s", prepared.provider.Name())
	}
}

func TestPrepareRequest_ProviderSelectionGemini(t *testing.T) {
	mockGemini := newMockProvider("gemini")
	svc := createChatServiceWithMocks(newMockProvider("openai"), mockGemini, newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("gemini")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "Hello",
		PreferredProvider: pb.Provider_PROVIDER_GEMINI,
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}
	if prepared.provider.Name() != "gemini" {
		t.Errorf("expected gemini provider, got %s", prepared.provider.Name())
	}
}

func TestPrepareRequest_ProviderSelectionAnthropic(t *testing.T) {
	mockAnthropic := newMockProvider("anthropic")
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), mockAnthropic, nil)
	tenantCfg := createTestTenantConfig("anthropic")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "Hello",
		PreferredProvider: pb.Provider_PROVIDER_ANTHROPIC,
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}
	if prepared.provider.Name() != "anthropic" {
		t.Errorf("expected anthropic provider, got %s", prepared.provider.Name())
	}
}

func TestPrepareRequest_ProviderNotEnabled(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai") // Only openai enabled
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "Hello",
		PreferredProvider: pb.Provider_PROVIDER_GEMINI, // Not enabled for tenant
	}

	_, err := svc.prepareRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error for provider not enabled")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("expected error about provider not enabled, got: %v", err)
	}
}

func TestPrepareRequest_DefaultProviderSelection(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai", "gemini")
	tenantCfg.Failover.Enabled = true
	tenantCfg.Failover.Order = []string{"gemini", "openai"}
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "Hello",
		PreferredProvider: pb.Provider_PROVIDER_UNSPECIFIED,
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}
	// Should select gemini (first in failover order)
	if prepared.provider.Name() != "gemini" {
		t.Errorf("expected gemini provider (first in failover), got %s", prepared.provider.Name())
	}
}

func TestPrepareRequest_ValidRequestID(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput: "Hello",
		RequestId: "valid-request-id_123",
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}
	if prepared.requestID != "valid-request-id_123" {
		t.Errorf("expected request ID 'valid-request-id_123', got %s", prepared.requestID)
	}
}

func TestPrepareRequest_GeneratesRequestID(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput: "Hello",
		// No RequestId provided
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}
	if prepared.requestID == "" {
		t.Error("expected generated request ID")
	}
	if len(prepared.requestID) < 16 {
		t.Errorf("expected request ID length >= 16, got %d", len(prepared.requestID))
	}
}

func TestPrepareRequest_BuildsParams(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:    "Hello world",
		Instructions: "Be helpful",
		ClientId:     "client-123",
		ConversationHistory: []*pb.Message{
			{Role: "user", Content: "Previous message", Timestamp: 1000},
		},
		EnableWebSearch:  true,
		EnableFileSearch: false,
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}

	if prepared.params.UserInput != "Hello world" {
		t.Errorf("expected UserInput 'Hello world', got %s", prepared.params.UserInput)
	}
	if prepared.params.Instructions != "Be helpful" {
		t.Errorf("expected Instructions 'Be helpful', got %s", prepared.params.Instructions)
	}
	if prepared.params.ClientID != "client-123" {
		t.Errorf("expected ClientID 'client-123', got %s", prepared.params.ClientID)
	}
	if prepared.params.EnableWebSearch != true {
		t.Error("expected EnableWebSearch true")
	}
	if prepared.params.EnableFileSearch != false {
		t.Error("expected EnableFileSearch false")
	}
	if len(prepared.params.ConversationHistory) != 1 {
		t.Errorf("expected 1 history message, got %d", len(prepared.params.ConversationHistory))
	}
}

// ==================== RAG Integration Tests ====================

func TestPrepareRequest_RAGContextInjectedForNonOpenAI(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockExtractor := testutil.NewMockExtractor()

	// Create store and add test data
	mockStore.CreateCollection(context.Background(), "test-tenant_test-store", 768)
	mockStore.Upsert(context.Background(), "test-tenant_test-store", []vectorstore.Point{
		{
			ID:     "chunk1",
			Vector: make([]float32, 768),
			Payload: map[string]any{
				"text":     "This is relevant context from the document.",
				"filename": "test.pdf",
			},
		},
	})

	ragService := rag.NewService(mockEmbedder, mockStore, mockExtractor, rag.DefaultServiceOptions())

	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), ragService)
	tenantCfg := createTestTenantConfig("gemini")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "What does the document say?",
		Instructions:      "Original instructions",
		PreferredProvider: pb.Provider_PROVIDER_GEMINI,
		EnableFileSearch:  true,
		FileStoreId:       "test-store",
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}

	// Check that RAG context was retrieved
	if len(prepared.ragChunks) == 0 {
		t.Error("expected RAG chunks to be retrieved")
	}

	// Check that instructions were modified to include RAG context
	if !strings.Contains(prepared.params.Instructions, "Original instructions") {
		t.Error("expected original instructions to be preserved")
	}
	if !strings.Contains(prepared.params.Instructions, "Relevant context") {
		t.Error("expected RAG context to be injected into instructions")
	}
}

func TestPrepareRequest_RAGNotInjectedForOpenAI(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockExtractor := testutil.NewMockExtractor()

	mockStore.CreateCollection(context.Background(), "test-tenant_test-store", 768)
	mockStore.Upsert(context.Background(), "test-tenant_test-store", []vectorstore.Point{
		{
			ID:     "chunk1",
			Vector: make([]float32, 768),
			Payload: map[string]any{
				"text":     "This is relevant context.",
				"filename": "test.pdf",
			},
		},
	})

	ragService := rag.NewService(mockEmbedder, mockStore, mockExtractor, rag.DefaultServiceOptions())

	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), ragService)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "What does the document say?",
		Instructions:      "Original instructions",
		PreferredProvider: pb.Provider_PROVIDER_OPENAI,
		EnableFileSearch:  true,
		FileStoreId:       "test-store",
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}

	// OpenAI handles file search natively, so no RAG injection
	if len(prepared.ragChunks) != 0 {
		t.Error("expected no RAG chunks for OpenAI (it handles file search natively)")
	}
	if strings.Contains(prepared.params.Instructions, "Relevant context") {
		t.Error("expected no RAG context injection for OpenAI")
	}
}

func TestPrepareRequest_NoRAGWithoutFileSearch(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockExtractor := testutil.NewMockExtractor()
	ragService := rag.NewService(mockEmbedder, mockStore, mockExtractor, rag.DefaultServiceOptions())

	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), ragService)
	tenantCfg := createTestTenantConfig("gemini")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "Hello",
		Instructions:      "Original instructions",
		PreferredProvider: pb.Provider_PROVIDER_GEMINI,
		EnableFileSearch:  false, // File search disabled
		FileStoreId:       "test-store",
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}

	if len(prepared.ragChunks) != 0 {
		t.Error("expected no RAG chunks when file search disabled")
	}
}

func TestPrepareRequest_NoRAGWithoutStoreID(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockExtractor := testutil.NewMockExtractor()
	ragService := rag.NewService(mockEmbedder, mockStore, mockExtractor, rag.DefaultServiceOptions())

	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), ragService)
	tenantCfg := createTestTenantConfig("gemini")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "Hello",
		Instructions:      "Original instructions",
		PreferredProvider: pb.Provider_PROVIDER_GEMINI,
		EnableFileSearch:  true,
		FileStoreId:       "", // No store ID
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}

	if len(prepared.ragChunks) != 0 {
		t.Error("expected no RAG chunks when no store ID")
	}
}

func TestPrepareRequest_NoRAGServiceConfigured(t *testing.T) {
	// RAG service is nil
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("gemini")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput:         "Hello",
		Instructions:      "Original instructions",
		PreferredProvider: pb.Provider_PROVIDER_GEMINI,
		EnableFileSearch:  true,
		FileStoreId:       "test-store",
	}

	prepared, err := svc.prepareRequest(ctx, req)
	if err != nil {
		t.Fatalf("prepareRequest failed: %v", err)
	}

	// Should succeed but without RAG
	if len(prepared.ragChunks) != 0 {
		t.Error("expected no RAG chunks when RAG service is nil")
	}
}

// ==================== buildProviderConfig Tests ====================

func TestBuildProviderConfig_FromTenant(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)

	temp := 0.7
	topP := 0.9
	maxTokens := 1000
	tenantCfg := &tenant.TenantConfig{
		TenantID: "test-tenant",
		Providers: map[string]tenant.ProviderConfig{
			"openai": {
				Enabled:         true,
				APIKey:          "tenant-api-key",
				Model:           "gpt-4",
				Temperature:     &temp,
				TopP:            &topP,
				MaxOutputTokens: &maxTokens,
				BaseURL:         "https://tenant-base.example.com",
			},
		},
	}
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		UserInput: "Hello",
	}

	cfg := svc.buildProviderConfig(ctx, req, "openai")

	if cfg.APIKey != "tenant-api-key" {
		t.Errorf("expected tenant API key, got %s", cfg.APIKey)
	}
	if cfg.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %s", cfg.Model)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", cfg.Temperature)
	}
	if cfg.TopP == nil || *cfg.TopP != 0.9 {
		t.Errorf("expected topP 0.9, got %v", cfg.TopP)
	}
	if cfg.MaxOutputTokens == nil || *cfg.MaxOutputTokens != 1000 {
		t.Errorf("expected maxTokens 1000, got %v", cfg.MaxOutputTokens)
	}
	if cfg.BaseURL != "https://tenant-base.example.com" {
		t.Errorf("expected tenant base URL, got %s", cfg.BaseURL)
	}
}

func TestBuildProviderConfig_RequestOverrides(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)

	temp := 0.7
	tenantCfg := &tenant.TenantConfig{
		TenantID: "test-tenant",
		Providers: map[string]tenant.ProviderConfig{
			"openai": {
				Enabled:     true,
				APIKey:      "tenant-api-key",
				Model:       "gpt-3.5-turbo",
				Temperature: &temp,
			},
		},
	}
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	reqTemp := float64(0.9)
	reqMaxTokens := int32(2000)
	req := &pb.GenerateReplyRequest{
		UserInput: "Hello",
		ProviderConfigs: map[string]*pb.ProviderConfig{
			"openai": {
				Model:           "gpt-4-turbo",
				Temperature:     &reqTemp,
				MaxOutputTokens: &reqMaxTokens,
				BaseUrl:         "https://request-base.example.com",
			},
		},
	}

	cfg := svc.buildProviderConfig(ctx, req, "openai")

	// API key should come from tenant, not request (security)
	if cfg.APIKey != "tenant-api-key" {
		t.Errorf("expected tenant API key (security), got %s", cfg.APIKey)
	}
	// Other values should be overridden by request
	if cfg.Model != "gpt-4-turbo" {
		t.Errorf("expected request model override, got %s", cfg.Model)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.9 {
		t.Errorf("expected request temperature 0.9, got %v", cfg.Temperature)
	}
	if cfg.MaxOutputTokens == nil || *cfg.MaxOutputTokens != 2000 {
		t.Errorf("expected request maxTokens 2000, got %v", cfg.MaxOutputTokens)
	}
	if cfg.BaseURL != "https://request-base.example.com" {
		t.Errorf("expected request base URL, got %s", cfg.BaseURL)
	}
}

func TestBuildProviderConfig_NoTenant(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)

	// No tenant in context
	ctx := context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    "test-client",
		Permissions: []auth.Permission{auth.PermissionChat},
	})

	reqTemp := float64(0.5)
	req := &pb.GenerateReplyRequest{
		UserInput: "Hello",
		ProviderConfigs: map[string]*pb.ProviderConfig{
			"openai": {
				Model:       "gpt-4",
				Temperature: &reqTemp,
			},
		},
	}

	cfg := svc.buildProviderConfig(ctx, req, "openai")

	// Should use request values when no tenant
	if cfg.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4' from request, got %s", cfg.Model)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5 from request, got %v", cfg.Temperature)
	}
}

// ==================== selectProviderWithTenant Tests ====================

func TestSelectProviderWithTenant_ReturnsOpenAI(t *testing.T) {
	mockOpenAI := newMockProvider("openai")
	svc := createChatServiceWithMocks(mockOpenAI, newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		PreferredProvider: pb.Provider_PROVIDER_OPENAI,
	}

	p, err := svc.selectProviderWithTenant(ctx, req)
	if err != nil {
		t.Fatalf("selectProviderWithTenant failed: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("expected openai, got %s", p.Name())
	}
}

func TestSelectProviderWithTenant_ReturnsGemini(t *testing.T) {
	mockGemini := newMockProvider("gemini")
	svc := createChatServiceWithMocks(newMockProvider("openai"), mockGemini, newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("gemini")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		PreferredProvider: pb.Provider_PROVIDER_GEMINI,
	}

	p, err := svc.selectProviderWithTenant(ctx, req)
	if err != nil {
		t.Fatalf("selectProviderWithTenant failed: %v", err)
	}
	if p.Name() != "gemini" {
		t.Errorf("expected gemini, got %s", p.Name())
	}
}

func TestSelectProviderWithTenant_ReturnsAnthropic(t *testing.T) {
	mockAnthropic := newMockProvider("anthropic")
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), mockAnthropic, nil)
	tenantCfg := createTestTenantConfig("anthropic")
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		PreferredProvider: pb.Provider_PROVIDER_ANTHROPIC,
	}

	p, err := svc.selectProviderWithTenant(ctx, req)
	if err != nil {
		t.Fatalf("selectProviderWithTenant failed: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("expected anthropic, got %s", p.Name())
	}
}

func TestSelectProviderWithTenant_DefaultsToOpenAI(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)

	// No tenant config - should default to openai
	ctx := context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    "test-client",
		Permissions: []auth.Permission{auth.PermissionChat},
	})

	req := &pb.GenerateReplyRequest{
		PreferredProvider: pb.Provider_PROVIDER_UNSPECIFIED,
	}

	p, err := svc.selectProviderWithTenant(ctx, req)
	if err != nil {
		t.Fatalf("selectProviderWithTenant failed: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("expected openai as default, got %s", p.Name())
	}
}

func TestSelectProviderWithTenant_UsesFailoverOrder(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai", "gemini", "anthropic")
	tenantCfg.Failover.Enabled = true
	tenantCfg.Failover.Order = []string{"anthropic", "gemini", "openai"}
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		PreferredProvider: pb.Provider_PROVIDER_UNSPECIFIED,
	}

	p, err := svc.selectProviderWithTenant(ctx, req)
	if err != nil {
		t.Fatalf("selectProviderWithTenant failed: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("expected anthropic (first in failover order), got %s", p.Name())
	}
}

func TestSelectProviderWithTenant_ProviderNotEnabled(t *testing.T) {
	svc := createChatServiceWithMocks(newMockProvider("openai"), newMockProvider("gemini"), newMockProvider("anthropic"), nil)
	tenantCfg := createTestTenantConfig("openai") // Only openai enabled
	ctx := ctxWithChatPermissionAndTenant("test-client", tenantCfg)

	req := &pb.GenerateReplyRequest{
		PreferredProvider: pb.Provider_PROVIDER_ANTHROPIC, // Not enabled
	}

	_, err := svc.selectProviderWithTenant(ctx, req)
	if err == nil {
		t.Fatal("expected error for disabled provider")
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Errorf("expected 'not enabled' error, got: %v", err)
	}
}

// ==================== getFallbackProvider Tests ====================

func TestGetFallbackProvider_SpecifiedFallback(t *testing.T) {
	mockGemini := newMockProvider("gemini")
	svc := createChatServiceWithMocks(newMockProvider("openai"), mockGemini, newMockProvider("anthropic"), nil)

	fallback := svc.getFallbackProvider("openai", pb.Provider_PROVIDER_GEMINI)
	if fallback == nil {
		t.Fatal("expected fallback provider")
	}
	if fallback.Name() != "gemini" {
		t.Errorf("expected gemini fallback, got %s", fallback.Name())
	}
}

func TestGetFallbackProvider_DefaultFallbackFromOpenAI(t *testing.T) {
	mockGemini := newMockProvider("gemini")
	svc := createChatServiceWithMocks(newMockProvider("openai"), mockGemini, newMockProvider("anthropic"), nil)

	fallback := svc.getFallbackProvider("openai", pb.Provider_PROVIDER_UNSPECIFIED)
	if fallback == nil {
		t.Fatal("expected fallback provider")
	}
	if fallback.Name() != "gemini" {
		t.Errorf("expected gemini as default fallback from openai, got %s", fallback.Name())
	}
}

func TestGetFallbackProvider_DefaultFallbackFromGemini(t *testing.T) {
	mockOpenAI := newMockProvider("openai")
	svc := createChatServiceWithMocks(mockOpenAI, newMockProvider("gemini"), newMockProvider("anthropic"), nil)

	fallback := svc.getFallbackProvider("gemini", pb.Provider_PROVIDER_UNSPECIFIED)
	if fallback == nil {
		t.Fatal("expected fallback provider")
	}
	if fallback.Name() != "openai" {
		t.Errorf("expected openai as default fallback from gemini, got %s", fallback.Name())
	}
}

func TestGetFallbackProvider_DefaultFallbackFromAnthropic(t *testing.T) {
	mockOpenAI := newMockProvider("openai")
	svc := createChatServiceWithMocks(mockOpenAI, newMockProvider("gemini"), newMockProvider("anthropic"), nil)

	fallback := svc.getFallbackProvider("anthropic", pb.Provider_PROVIDER_UNSPECIFIED)
	if fallback == nil {
		t.Fatal("expected fallback provider")
	}
	if fallback.Name() != "openai" {
		t.Errorf("expected openai as default fallback from anthropic, got %s", fallback.Name())
	}
}

// ==================== convertHistory Tests ====================

func TestConvertHistory_Empty(t *testing.T) {
	result := convertHistory(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	result = convertHistory([]*pb.Message{})
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestConvertHistory_MultipleMessages(t *testing.T) {
	msgs := []*pb.Message{
		{Role: "user", Content: "Hello", Timestamp: 1000},
		{Role: "assistant", Content: "Hi there!", Timestamp: 1001},
		{Role: "user", Content: "How are you?", Timestamp: 1002},
	}

	result := convertHistory(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	if result[0].Role != "user" {
		t.Errorf("expected role 'user', got %s", result[0].Role)
	}
	if result[0].Content != "Hello" {
		t.Errorf("expected content 'Hello', got %s", result[0].Content)
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %s", result[1].Role)
	}
}

// ==================== mapProviderToProto Tests ====================

func TestMapProviderToProto(t *testing.T) {
	tests := []struct {
		input    string
		expected pb.Provider
	}{
		{"openai", pb.Provider_PROVIDER_OPENAI},
		{"gemini", pb.Provider_PROVIDER_GEMINI},
		{"anthropic", pb.Provider_PROVIDER_ANTHROPIC},
		{"unknown", pb.Provider_PROVIDER_UNSPECIFIED},
		{"", pb.Provider_PROVIDER_UNSPECIFIED},
	}

	for _, tc := range tests {
		result := mapProviderToProto(tc.input)
		if result != tc.expected {
			t.Errorf("mapProviderToProto(%q) = %v, expected %v", tc.input, result, tc.expected)
		}
	}
}
