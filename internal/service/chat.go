package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cliffpyles/aibox/internal/auth"
	sanitize "github.com/cliffpyles/aibox/internal/errors"
	"github.com/cliffpyles/aibox/internal/provider"
	"github.com/cliffpyles/aibox/internal/provider/anthropic"
	"github.com/cliffpyles/aibox/internal/provider/gemini"
	"github.com/cliffpyles/aibox/internal/provider/openai"
	"github.com/cliffpyles/aibox/internal/rag"
	"github.com/cliffpyles/aibox/internal/validation"
	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ChatService implements the AIBoxService gRPC service.
type ChatService struct {
	pb.UnimplementedAIBoxServiceServer

	openaiProvider    provider.Provider
	geminiProvider    provider.Provider
	anthropicProvider provider.Provider
	rateLimiter       *auth.RateLimiter
	ragService        *rag.Service
}

// NewChatService creates a new chat service.
// The ragService parameter is optional - pass nil to disable self-hosted RAG.
func NewChatService(rateLimiter *auth.RateLimiter, ragService *rag.Service) *ChatService {
	return &ChatService{
		openaiProvider:    openai.NewClient(),
		geminiProvider:    gemini.NewClient(),
		anthropicProvider: anthropic.NewClient(),
		rateLimiter:       rateLimiter,
		ragService:        ragService,
	}
}

// GenerateReply generates a completion.
func (s *ChatService) GenerateReply(ctx context.Context, req *pb.GenerateReplyRequest) (*pb.GenerateReplyResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionChat); err != nil {
		return nil, err
	}

	// Validate input sizes
	if err := validation.ValidateGenerateRequest(
		req.UserInput,
		req.Instructions,
		len(req.ConversationHistory),
	); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate metadata
	if err := validation.ValidateMetadata(req.Metadata); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate or generate request ID
	requestID, err := validation.ValidateOrGenerateRequestID(req.RequestId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate request
	if strings.TrimSpace(req.UserInput) == "" {
		return nil, status.Error(codes.InvalidArgument, "user_input is required")
	}

	// Select provider (now with tenant awareness)
	selectedProvider, err := s.selectProviderWithTenant(ctx, req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid provider: %v", err)
	}

	// Build provider config (from tenant + request overrides)
	providerCfg := s.buildProviderConfig(ctx, req, selectedProvider.Name())

	// Retrieve RAG context for non-OpenAI providers
	var ragChunks []rag.RetrieveResult
	instructions := req.Instructions
	if req.EnableFileSearch && strings.TrimSpace(req.FileStoreId) != "" && selectedProvider.Name() != "openai" {
		chunks, err := s.retrieveRAGContext(ctx, req.FileStoreId, req.UserInput)
		if err != nil {
			slog.Warn("RAG retrieval failed, continuing without context",
				"error", err,
				"store_id", req.FileStoreId,
			)
		} else if len(chunks) > 0 {
			ragChunks = chunks
			ragContext := formatRAGContext(chunks)
			instructions = instructions + ragContext
			slog.Info("injected RAG context",
				"store_id", req.FileStoreId,
				"chunks", len(chunks),
			)
		}
	}

	// Build params
	params := provider.GenerateParams{
		Instructions:        instructions, // May include RAG context for non-OpenAI
		UserInput:           req.UserInput,
		ConversationHistory: convertHistory(req.ConversationHistory),
		FileStoreID:         req.FileStoreId,
		PreviousResponseID:  req.PreviousResponseId,
		OverrideModel:       req.ModelOverride,
		EnableWebSearch:     req.EnableWebSearch,
		EnableFileSearch:    req.EnableFileSearch,
		FileIDToFilename:    req.FileIdToFilename,
		Config:              providerCfg,
		RequestID:           requestID,
		ClientID:            req.ClientId,
	}

	slog.Info("generating reply",
		"provider", selectedProvider.Name(),
		"model", providerCfg.Model,
		"request_id", requestID,
		"client_id", req.ClientId,
	)

	// Generate reply
	result, err := selectedProvider.GenerateReply(ctx, params)
	if err != nil {
		// Try failover if enabled
		if req.EnableFailover {
			fallbackProvider := s.getFallbackProvider(selectedProvider.Name(), req.FallbackProvider)
			if fallbackProvider != nil {
				slog.Warn("primary provider failed, trying fallback",
					"primary", selectedProvider.Name(),
					"fallback", fallbackProvider.Name(),
					"error", err,
				)

				params.Config = s.buildProviderConfig(ctx, req, fallbackProvider.Name())
				fallbackResult, fallbackErr := fallbackProvider.GenerateReply(ctx, params)
				if fallbackErr == nil {
					return s.buildResponse(fallbackResult, fallbackProvider.Name(), true, selectedProvider.Name(), err.Error()), nil
				}
				// Return original error if fallback also fails
			}
		}
		slog.Error("provider request failed",
			"provider", selectedProvider.Name(),
			"error", err,
			"request_id", requestID,
		)
		return nil, status.Error(codes.Internal, sanitize.SanitizeForClient(err))
	}

	// Record token usage for rate limiting
	if s.rateLimiter != nil && result.Usage != nil {
		client := auth.ClientFromContext(ctx)
		if client != nil {
			_ = s.rateLimiter.RecordTokens(ctx, client.ClientID, result.Usage.TotalTokens, client.RateLimits.TokensPerMinute)
		}
	}

	// Add RAG citations to result if we used self-hosted RAG
	if len(ragChunks) > 0 {
		result.Citations = append(result.Citations, ragChunksToCitations(ragChunks)...)
	}

	return s.buildResponse(result, selectedProvider.Name(), false, "", ""), nil
}

// GenerateReplyStream generates a streaming completion.
func (s *ChatService) GenerateReplyStream(req *pb.GenerateReplyRequest, stream pb.AIBoxService_GenerateReplyStreamServer) error {
	ctx := stream.Context()

	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionChatStream); err != nil {
		return err
	}

	// Validate input sizes
	if err := validation.ValidateGenerateRequest(
		req.UserInput,
		req.Instructions,
		len(req.ConversationHistory),
	); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate metadata
	if err := validation.ValidateMetadata(req.Metadata); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate or generate request ID
	requestID, err := validation.ValidateOrGenerateRequestID(req.RequestId)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate request
	if strings.TrimSpace(req.UserInput) == "" {
		return status.Error(codes.InvalidArgument, "user_input is required")
	}

	// Select provider (now with tenant awareness)
	selectedProvider, err := s.selectProviderWithTenant(ctx, req)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid provider: %v", err)
	}

	// Build provider config (from tenant + request overrides)
	providerCfg := s.buildProviderConfig(ctx, req, selectedProvider.Name())

	// Retrieve RAG context for non-OpenAI providers
	var ragChunks []rag.RetrieveResult
	instructions := req.Instructions
	if req.EnableFileSearch && strings.TrimSpace(req.FileStoreId) != "" && selectedProvider.Name() != "openai" {
		chunks, err := s.retrieveRAGContext(ctx, req.FileStoreId, req.UserInput)
		if err != nil {
			slog.Warn("RAG retrieval failed, continuing without context",
				"error", err,
				"store_id", req.FileStoreId,
			)
		} else if len(chunks) > 0 {
			ragChunks = chunks
			ragContext := formatRAGContext(chunks)
			instructions = instructions + ragContext
			slog.Info("injected RAG context for stream",
				"store_id", req.FileStoreId,
				"chunks", len(chunks),
			)
		}
	}

	// Build params
	params := provider.GenerateParams{
		Instructions:        instructions, // May include RAG context for non-OpenAI
		UserInput:           req.UserInput,
		ConversationHistory: convertHistory(req.ConversationHistory),
		FileStoreID:         req.FileStoreId,
		PreviousResponseID:  req.PreviousResponseId,
		OverrideModel:       req.ModelOverride,
		EnableWebSearch:     req.EnableWebSearch,
		EnableFileSearch:    req.EnableFileSearch,
		FileIDToFilename:    req.FileIdToFilename,
		Config:              providerCfg,
		RequestID:           requestID,
		ClientID:            req.ClientId,
	}

	// Generate streaming reply
	streamChunks, err := selectedProvider.GenerateReplyStream(ctx, params)
	if err != nil {
		return status.Error(codes.Internal, sanitize.SanitizeForClient(err))
	}

	// Send RAG citations first if we have them
	for _, chunk := range ragChunks {
		snippet := chunk.Text
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		citation := provider.Citation{
			Type:     provider.CitationTypeFile,
			Provider: "qdrant",
			Filename: chunk.Filename,
			Snippet:  snippet,
		}
		pbChunk := &pb.GenerateReplyChunk{
			Chunk: &pb.GenerateReplyChunk_CitationUpdate{
				CitationUpdate: &pb.CitationUpdate{
					Citation: convertCitation(citation),
				},
			},
		}
		if err := stream.Send(pbChunk); err != nil {
			return err
		}
	}

	// Forward chunks from provider
	for chunk := range streamChunks {
		var pbChunk *pb.GenerateReplyChunk

		switch chunk.Type {
		case provider.ChunkTypeText:
			pbChunk = &pb.GenerateReplyChunk{
				Chunk: &pb.GenerateReplyChunk_TextDelta{
					TextDelta: &pb.TextDelta{
						Text:  chunk.Text,
						Index: int32(chunk.Index),
					},
				},
			}
		case provider.ChunkTypeUsage:
			pbChunk = &pb.GenerateReplyChunk{
				Chunk: &pb.GenerateReplyChunk_UsageUpdate{
					UsageUpdate: &pb.UsageUpdate{
						Usage: convertUsage(chunk.Usage),
					},
				},
			}
		case provider.ChunkTypeCitation:
			if chunk.Citation != nil {
				pbChunk = &pb.GenerateReplyChunk{
					Chunk: &pb.GenerateReplyChunk_CitationUpdate{
						CitationUpdate: &pb.CitationUpdate{
							Citation: convertCitation(*chunk.Citation),
						},
					},
				}
			}
		case provider.ChunkTypeComplete:
			// Record token usage for rate limiting on stream completion
			if s.rateLimiter != nil && chunk.Usage != nil {
				client := auth.ClientFromContext(ctx)
				if client != nil {
					_ = s.rateLimiter.RecordTokens(ctx, client.ClientID, chunk.Usage.TotalTokens, client.RateLimits.TokensPerMinute)
				}
			}
			pbChunk = &pb.GenerateReplyChunk{
				Chunk: &pb.GenerateReplyChunk_Complete{
					Complete: &pb.StreamComplete{
						ResponseId: chunk.ResponseID,
						Model:      chunk.Model,
						Provider:   mapProviderToProto(selectedProvider.Name()),
						FinalUsage: convertUsage(chunk.Usage),
					},
				},
			}
		case provider.ChunkTypeError:
			pbChunk = &pb.GenerateReplyChunk{
				Chunk: &pb.GenerateReplyChunk_Error{
					Error: &pb.StreamError{
						Code:      "PROVIDER_ERROR",
						Message:   sanitize.SanitizeForClient(chunk.Error),
						Retryable: chunk.Retryable,
					},
				},
			}
		}

		if pbChunk != nil {
			if err := stream.Send(pbChunk); err != nil {
				return err
			}
		}
	}

	return nil
}

// SelectProvider determines which provider to use.
func (s *ChatService) SelectProvider(ctx context.Context, req *pb.SelectProviderRequest) (*pb.SelectProviderResponse, error) {
	// Check for trigger phrases
	content := strings.ToLower(req.Content)
	for _, trigger := range req.Triggers {
		if strings.Contains(content, strings.ToLower(trigger.Phrase)) {
			return &pb.SelectProviderResponse{
				Provider:      trigger.Provider,
				ModelOverride: trigger.Model,
				Reason:        "trigger",
			}, nil
		}
	}

	// Check for continuity
	if req.ExistingProvider != "" {
		return &pb.SelectProviderResponse{
			Provider: mapProviderFromString(req.ExistingProvider),
			Reason:   "continuity",
		}, nil
	}

	// Default to OpenAI
	return &pb.SelectProviderResponse{
		Provider: pb.Provider_PROVIDER_OPENAI,
		Reason:   "default",
	}, nil
}

// selectProvider selects the appropriate provider.
func (s *ChatService) selectProvider(req *pb.GenerateReplyRequest) (provider.Provider, error) {
	switch req.PreferredProvider {
	case pb.Provider_PROVIDER_OPENAI:
		return s.openaiProvider, nil
	case pb.Provider_PROVIDER_GEMINI:
		return s.geminiProvider, nil
	case pb.Provider_PROVIDER_ANTHROPIC:
		return s.anthropicProvider, nil
	case pb.Provider_PROVIDER_UNSPECIFIED:
		return s.openaiProvider, nil // Default
	default:
		return nil, fmt.Errorf("unknown provider: %v", req.PreferredProvider)
	}
}

// getFallbackProvider returns a fallback provider.
func (s *ChatService) getFallbackProvider(primary string, specified pb.Provider) provider.Provider {
	if specified != pb.Provider_PROVIDER_UNSPECIFIED {
		switch specified {
		case pb.Provider_PROVIDER_OPENAI:
			return s.openaiProvider
		case pb.Provider_PROVIDER_GEMINI:
			return s.geminiProvider
		case pb.Provider_PROVIDER_ANTHROPIC:
			return s.anthropicProvider
		}
	}

	// Default fallback order
	switch primary {
	case "openai":
		return s.geminiProvider
	case "gemini":
		return s.openaiProvider
	case "anthropic":
		return s.openaiProvider
	default:
		return s.geminiProvider
	}
}

// buildProviderConfig builds provider config from tenant config and request overrides.
func (s *ChatService) buildProviderConfig(ctx context.Context, req *pb.GenerateReplyRequest, providerName string) provider.ProviderConfig {
	cfg := provider.ProviderConfig{}

	// First, try to get config from tenant
	tenantCfg := auth.TenantFromContext(ctx)
	if tenantCfg != nil {
		if pCfg, ok := tenantCfg.GetProvider(providerName); ok {
			cfg.APIKey = pCfg.APIKey
			cfg.Model = pCfg.Model
			cfg.Temperature = pCfg.Temperature
			cfg.TopP = pCfg.TopP
			cfg.MaxOutputTokens = pCfg.MaxOutputTokens
			cfg.BaseURL = pCfg.BaseURL
			cfg.ExtraOptions = pCfg.ExtraOptions
		}
	}

	// Then, allow request to override (except API key for security)
	if pbCfg, ok := req.ProviderConfigs[providerName]; ok {
		// SECURITY: API keys must come from server-side tenant config, not requests
		// if pbCfg.ApiKey != "" {
		// 	cfg.APIKey = pbCfg.ApiKey
		// }
		if pbCfg.Model != "" {
			cfg.Model = pbCfg.Model
		}
		if pbCfg.Temperature != nil {
			temp := *pbCfg.Temperature
			cfg.Temperature = &temp
		}
		if pbCfg.TopP != nil {
			topP := *pbCfg.TopP
			cfg.TopP = &topP
		}
		if pbCfg.MaxOutputTokens != nil {
			maxTokens := int(*pbCfg.MaxOutputTokens)
			cfg.MaxOutputTokens = &maxTokens
		}
		if pbCfg.BaseUrl != "" {
			cfg.BaseURL = pbCfg.BaseUrl
		}
		if len(pbCfg.ExtraOptions) > 0 {
			if cfg.ExtraOptions == nil {
				cfg.ExtraOptions = make(map[string]string)
			}
			for k, v := range pbCfg.ExtraOptions {
				cfg.ExtraOptions[k] = v
			}
		}
	}

	return cfg
}

// selectProviderWithTenant selects provider using tenant config for validation.
func (s *ChatService) selectProviderWithTenant(ctx context.Context, req *pb.GenerateReplyRequest) (provider.Provider, error) {
	tenantCfg := auth.TenantFromContext(ctx)

	// Determine which provider to use
	var providerName string
	switch req.PreferredProvider {
	case pb.Provider_PROVIDER_OPENAI:
		providerName = "openai"
	case pb.Provider_PROVIDER_GEMINI:
		providerName = "gemini"
	case pb.Provider_PROVIDER_ANTHROPIC:
		providerName = "anthropic"
	case pb.Provider_PROVIDER_UNSPECIFIED:
		// Try to get default from tenant config
		if tenantCfg != nil {
			if name, _, ok := tenantCfg.DefaultProvider(); ok {
				providerName = name
			}
		}
		if providerName == "" {
			providerName = "openai" // Default
		}
	default:
		return nil, fmt.Errorf("unknown provider: %v", req.PreferredProvider)
	}

	// Validate provider is enabled for tenant (if tenant exists)
	// SECURITY: Removed API key override bypass - providers must be enabled in tenant config
	if tenantCfg != nil {
		if _, ok := tenantCfg.GetProvider(providerName); !ok {
			return nil, fmt.Errorf("provider %s not enabled for tenant", providerName)
		}
	}

	switch providerName {
	case "openai":
		return s.openaiProvider, nil
	case "gemini":
		return s.geminiProvider, nil
	case "anthropic":
		return s.anthropicProvider, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}
}


// retrieveRAGContext retrieves relevant document chunks for non-OpenAI providers.
// Returns nil if RAG is disabled, not configured, or provider is OpenAI.
func (s *ChatService) retrieveRAGContext(ctx context.Context, storeID, query string) ([]rag.RetrieveResult, error) {
	if s.ragService == nil {
		return nil, nil
	}
	if strings.TrimSpace(storeID) == "" {
		return nil, nil
	}

	return s.ragService.Retrieve(ctx, rag.RetrieveParams{
		StoreID:  storeID,
		TenantID: auth.TenantIDFromContext(ctx),
		Query:    query,
		TopK:     5,
	})
}

// formatRAGContext formats retrieved chunks for injection into the system prompt.
func formatRAGContext(chunks []rag.RetrieveResult) string {
	if len(chunks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n---\nRelevant context from uploaded documents:\n\n")

	for i, chunk := range chunks {
		sb.WriteString(fmt.Sprintf("[%d] From %s:\n%s\n\n", i+1, chunk.Filename, chunk.Text))
	}

	sb.WriteString("---\n\nUse the above context to help answer the user's question when relevant.\n")
	return sb.String()
}

// ragChunksToCitations converts RAG retrieval results to provider citations.
func ragChunksToCitations(chunks []rag.RetrieveResult) []provider.Citation {
	citations := make([]provider.Citation, len(chunks))
	for i, chunk := range chunks {
		snippet := chunk.Text
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		citations[i] = provider.Citation{
			Type:     provider.CitationTypeFile,
			Provider: "qdrant",
			Filename: chunk.Filename,
			Snippet:  snippet,
		}
	}
	return citations
}

// buildResponse builds a gRPC response from provider result.
func (s *ChatService) buildResponse(result provider.GenerateResult, providerName string, failedOver bool, originalProvider, originalError string) *pb.GenerateReplyResponse {
	resp := &pb.GenerateReplyResponse{
		Text:       result.Text,
		ResponseId: result.ResponseID,
		Usage:      convertUsage(result.Usage),
		Model:      result.Model,
		Provider:   mapProviderToProto(providerName),
	}

	for _, c := range result.Citations {
		resp.Citations = append(resp.Citations, convertCitation(c))
	}

	if failedOver {
		resp.FailedOver = true
		resp.OriginalProvider = mapProviderToProto(originalProvider)
		resp.OriginalError = originalError
	}

	return resp
}

// Helper functions

func convertHistory(msgs []*pb.Message) []provider.Message {
	var result []provider.Message
	for _, m := range msgs {
		result = append(result, provider.Message{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: time.Unix(m.Timestamp, 0),
		})
	}
	return result
}

func convertUsage(u *provider.Usage) *pb.Usage {
	if u == nil {
		return nil
	}
	return &pb.Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		TotalTokens:  u.TotalTokens,
	}
}

func convertCitation(c provider.Citation) *pb.Citation {
	var citationType pb.Citation_Type
	switch c.Type {
	case provider.CitationTypeURL:
		citationType = pb.Citation_TYPE_URL
	case provider.CitationTypeFile:
		citationType = pb.Citation_TYPE_FILE
	default:
		citationType = pb.Citation_TYPE_UNSPECIFIED
	}

	return &pb.Citation{
		Type:       citationType,
		Provider:   c.Provider,
		Url:        c.URL,
		Title:      c.Title,
		FileId:     c.FileID,
		Filename:   c.Filename,
		Snippet:    c.Snippet,
		StartIndex: int32(c.StartIndex),
		EndIndex:   int32(c.EndIndex),
		BrokenLink: c.BrokenLink,
	}
}

func mapProviderToProto(name string) pb.Provider {
	switch name {
	case "openai":
		return pb.Provider_PROVIDER_OPENAI
	case "gemini":
		return pb.Provider_PROVIDER_GEMINI
	case "anthropic":
		return pb.Provider_PROVIDER_ANTHROPIC
	default:
		return pb.Provider_PROVIDER_UNSPECIFIED
	}
}

func mapProviderFromString(name string) pb.Provider {
	return mapProviderToProto(name)
}
