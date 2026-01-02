package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cliffpyles/aibox/internal/auth"
	"github.com/cliffpyles/aibox/internal/provider"
	"github.com/cliffpyles/aibox/internal/provider/anthropic"
	"github.com/cliffpyles/aibox/internal/provider/gemini"
	"github.com/cliffpyles/aibox/internal/provider/openai"
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
}

// NewChatService creates a new chat service.
func NewChatService(rateLimiter *auth.RateLimiter) *ChatService {
	return &ChatService{
		openaiProvider:    openai.NewClient(),
		geminiProvider:    gemini.NewClient(),
		anthropicProvider: anthropic.NewClient(),
		rateLimiter:       rateLimiter,
	}
}

// GenerateReply generates a completion.
func (s *ChatService) GenerateReply(ctx context.Context, req *pb.GenerateReplyRequest) (*pb.GenerateReplyResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionChat); err != nil {
		return nil, err
	}

	// Validate request
	if strings.TrimSpace(req.UserInput) == "" {
		return nil, status.Error(codes.InvalidArgument, "user_input is required")
	}

	// Select provider
	selectedProvider, err := s.selectProvider(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid provider: %v", err)
	}

	// Build provider config
	providerCfg := s.buildProviderConfig(req, selectedProvider.Name())

	// Build params
	params := provider.GenerateParams{
		Instructions:        req.Instructions,
		UserInput:           req.UserInput,
		ConversationHistory: convertHistory(req.ConversationHistory),
		FileStoreID:         req.FileStoreId,
		PreviousResponseID:  req.PreviousResponseId,
		OverrideModel:       req.ModelOverride,
		EnableWebSearch:     req.EnableWebSearch,
		EnableFileSearch:    req.EnableFileSearch,
		FileIDToFilename:    req.FileIdToFilename,
		Config:              providerCfg,
		RequestID:           req.RequestId,
		ClientID:            req.ClientId,
	}

	slog.Info("generating reply",
		"provider", selectedProvider.Name(),
		"model", providerCfg.Model,
		"request_id", req.RequestId,
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

				params.Config = s.buildProviderConfig(req, fallbackProvider.Name())
				fallbackResult, fallbackErr := fallbackProvider.GenerateReply(ctx, params)
				if fallbackErr == nil {
					return s.buildResponse(fallbackResult, fallbackProvider.Name(), true, selectedProvider.Name(), err.Error()), nil
				}
				// Return original error if fallback also fails
			}
		}
		return nil, status.Errorf(codes.Internal, "provider error: %v", err)
	}

	// Record token usage for rate limiting
	if s.rateLimiter != nil && result.Usage != nil {
		client := auth.ClientFromContext(ctx)
		if client != nil {
			_ = s.rateLimiter.RecordTokens(ctx, client.ClientID, result.Usage.TotalTokens, client.RateLimits.TokensPerMinute)
		}
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

	// Validate request
	if strings.TrimSpace(req.UserInput) == "" {
		return status.Error(codes.InvalidArgument, "user_input is required")
	}

	// Select provider
	selectedProvider, err := s.selectProvider(req)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid provider: %v", err)
	}

	// Build provider config
	providerCfg := s.buildProviderConfig(req, selectedProvider.Name())

	// Build params
	params := provider.GenerateParams{
		Instructions:        req.Instructions,
		UserInput:           req.UserInput,
		ConversationHistory: convertHistory(req.ConversationHistory),
		FileStoreID:         req.FileStoreId,
		PreviousResponseID:  req.PreviousResponseId,
		OverrideModel:       req.ModelOverride,
		EnableWebSearch:     req.EnableWebSearch,
		EnableFileSearch:    req.EnableFileSearch,
		FileIDToFilename:    req.FileIdToFilename,
		Config:              providerCfg,
		RequestID:           req.RequestId,
		ClientID:            req.ClientId,
	}

	// Generate streaming reply
	chunks, err := selectedProvider.GenerateReplyStream(ctx, params)
	if err != nil {
		return status.Errorf(codes.Internal, "stream init error: %v", err)
	}

	// Forward chunks
	for chunk := range chunks {
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
						Message:   chunk.Error.Error(),
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

// buildProviderConfig builds provider config from request.
func (s *ChatService) buildProviderConfig(req *pb.GenerateReplyRequest, providerName string) provider.ProviderConfig {
	cfg := provider.ProviderConfig{}

	if pbCfg, ok := req.ProviderConfigs[providerName]; ok {
		cfg.APIKey = pbCfg.ApiKey
		cfg.Model = pbCfg.Model
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
		cfg.BaseURL = pbCfg.BaseUrl
		cfg.ExtraOptions = pbCfg.ExtraOptions
	}

	return cfg
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
