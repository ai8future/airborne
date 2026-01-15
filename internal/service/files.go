package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/auth"
	"github.com/ai8future/airborne/internal/provider/gemini"
	"github.com/ai8future/airborne/internal/provider/openai"
	"github.com/ai8future/airborne/internal/rag"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxUploadBytes is the maximum allowed file upload size (100MB).
const maxUploadBytes int64 = 100 * 1024 * 1024

// uploadTimeout is the maximum duration allowed for a file upload stream.
const uploadTimeout = 5 * time.Minute

// generateFileID creates a unique file identifier.
func generateFileID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "file_" + hex.EncodeToString(buf), nil
}

// FileService implements the FileService gRPC service for RAG file management.
type FileService struct {
	pb.UnimplementedFileServiceServer

	ragService  *rag.Service
	rateLimiter *auth.RateLimiter
}

// NewFileService creates a new file service.
func NewFileService(ragService *rag.Service, rateLimiter *auth.RateLimiter) *FileService {
	return &FileService{
		ragService:  ragService,
		rateLimiter: rateLimiter,
	}
}

// CreateFileStore creates a new vector store based on provider.
// - OpenAI: Creates OpenAI Vector Store
// - Gemini: Creates Gemini FileSearchStore
// - Unspecified/Internal: Creates Qdrant collection
func (s *FileService) CreateFileStore(ctx context.Context, req *pb.CreateFileStoreRequest) (*pb.CreateFileStoreResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return nil, err
	}

	// Route by provider
	switch req.Provider {
	case pb.Provider_PROVIDER_OPENAI:
		return s.createOpenAIVectorStore(ctx, req)
	case pb.Provider_PROVIDER_GEMINI:
		return s.createGeminiFileSearchStore(ctx, req)
	default:
		return s.createInternalStore(ctx, req)
	}
}

// createOpenAIVectorStore creates an OpenAI Vector Store.
func (s *FileService) createOpenAIVectorStore(ctx context.Context, req *pb.CreateFileStoreRequest) (*pb.CreateFileStoreResponse, error) {
	cfg := openai.FileStoreConfig{
		APIKey:         req.Config.GetApiKey(),
		BaseURL:        req.Config.GetBaseUrl(),
		ExpirationDays: int(req.ExpirationDays),
	}

	if cfg.APIKey == "" {
		return nil, status.Error(codes.InvalidArgument, "OpenAI API key is required for OpenAI vector stores")
	}

	result, err := openai.CreateVectorStore(ctx, cfg, req.Name)
	if err != nil {
		slog.Error("failed to create OpenAI vector store",
			"name", req.Name,
			"error", err,
		)
		return nil, fmt.Errorf("create OpenAI vector store: %w", err)
	}

	slog.Info("OpenAI vector store created",
		"store_id", result.StoreID,
		"name", result.Name,
	)

	return &pb.CreateFileStoreResponse{
		StoreId:   result.StoreID,
		Provider:  pb.Provider_PROVIDER_OPENAI,
		Name:      result.Name,
		CreatedAt: result.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// createGeminiFileSearchStore creates a Gemini FileSearchStore.
func (s *FileService) createGeminiFileSearchStore(ctx context.Context, req *pb.CreateFileStoreRequest) (*pb.CreateFileStoreResponse, error) {
	cfg := gemini.FileStoreConfig{
		APIKey:  req.Config.GetApiKey(),
		BaseURL: req.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return nil, status.Error(codes.InvalidArgument, "Gemini API key is required for Gemini file search stores")
	}

	result, err := gemini.CreateFileSearchStore(ctx, cfg, req.Name)
	if err != nil {
		slog.Error("failed to create Gemini file search store",
			"name", req.Name,
			"error", err,
		)
		return nil, fmt.Errorf("create Gemini file search store: %w", err)
	}

	slog.Info("Gemini file search store created",
		"store_id", result.StoreID,
		"name", result.Name,
	)

	return &pb.CreateFileStoreResponse{
		StoreId:   result.StoreID,
		Provider:  pb.Provider_PROVIDER_GEMINI,
		Name:      result.Name,
		CreatedAt: result.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// createInternalStore creates an internal Qdrant-based store.
func (s *FileService) createInternalStore(ctx context.Context, req *pb.CreateFileStoreRequest) (*pb.CreateFileStoreResponse, error) {
	// Generate store ID if name is provided, otherwise use a UUID-like ID
	storeID := req.Name
	if storeID == "" {
		storeID = fmt.Sprintf("store_%d", time.Now().UnixNano())
	}

	// Get tenant ID from auth context
	tenantID := auth.TenantIDFromContext(ctx)

	// Create the Qdrant collection via RAG service
	if err := s.ragService.CreateStore(ctx, tenantID, storeID); err != nil {
		slog.Error("failed to create file store",
			"tenant_id", tenantID,
			"store_id", storeID,
			"error", err,
		)
		return nil, fmt.Errorf("create store: %w", err)
	}

	slog.Info("file store created",
		"tenant_id", tenantID,
		"store_id", storeID,
	)

	return &pb.CreateFileStoreResponse{
		StoreId:   storeID,
		Provider:  pb.Provider_PROVIDER_UNSPECIFIED,
		Name:      req.Name,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// UploadFile uploads a file to a store using client streaming.
// Routes to appropriate backend based on provider in metadata.
func (s *FileService) UploadFile(stream pb.FileService_UploadFileServer) error {
	ctx := stream.Context()

	// Add upload timeout if context doesn't already have a deadline
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, uploadTimeout)
		defer cancel()
	}

	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return err
	}

	// Check rate limit for file uploads
	if s.rateLimiter != nil {
		client := auth.ClientFromContext(ctx)
		if client != nil {
			if err := s.rateLimiter.Allow(ctx, client); err != nil {
				return status.Error(codes.ResourceExhausted, "file upload rate limit exceeded")
			}
		}
	}

	// First message should be metadata
	firstMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("receive metadata: %w", err)
	}

	metadata := firstMsg.GetMetadata()
	if metadata == nil {
		return fmt.Errorf("first message must contain metadata")
	}

	if metadata.StoreId == "" {
		return fmt.Errorf("store_id is required")
	}
	if metadata.Filename == "" {
		return fmt.Errorf("filename is required")
	}

	// Validate declared size if provided
	if metadata.Size > 0 && metadata.Size > maxUploadBytes {
		return fmt.Errorf("file size %d exceeds maximum allowed size %d bytes", metadata.Size, maxUploadBytes)
	}

	slog.Info("starting file upload",
		"store_id", metadata.StoreId,
		"filename", metadata.Filename,
		"size", metadata.Size,
		"provider", metadata.Provider.String(),
	)

	// Collect file chunks with size limit enforcement
	// SECURITY: Use a temporary file instead of bytes.Buffer to prevent memory exhaustion (DoS)
	tmpFile, err := os.CreateTemp("", "airborne-upload-*.tmp")
	if err != nil {
		return status.Error(codes.Internal, "failed to create temporary file for upload")
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	var totalBytes int64
	for {
		// Check for context cancellation (timeout)
		select {
		case <-ctx.Done():
			return status.Error(codes.DeadlineExceeded, "upload timeout exceeded")
		default:
		}

		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("receive chunk: %w", err)
		}

		chunk := msg.GetChunk()
		if chunk == nil {
			continue
		}

		// Enforce size limit
		totalBytes += int64(len(chunk))
		if totalBytes > maxUploadBytes {
			return fmt.Errorf("file exceeds maximum allowed size %d bytes", maxUploadBytes)
		}

		if _, err := tmpFile.Write(chunk); err != nil {
			return fmt.Errorf("write to temp file: %w", err)
		}
	}

	// Reset file pointer to beginning for reading
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("seek temp file: %w", err)
	}

	// Route by provider
	switch metadata.Provider {
	case pb.Provider_PROVIDER_OPENAI:
		return s.uploadToOpenAI(ctx, stream, metadata, tmpFile)
	case pb.Provider_PROVIDER_GEMINI:
		return s.uploadToGemini(ctx, stream, metadata, tmpFile)
	default:
		return s.uploadToInternal(ctx, stream, metadata, tmpFile)
	}
}

// uploadToOpenAI uploads a file to an OpenAI Vector Store.
func (s *FileService) uploadToOpenAI(ctx context.Context, stream pb.FileService_UploadFileServer, metadata *pb.UploadFileMetadata, content io.Reader) error {
	cfg := openai.FileStoreConfig{
		APIKey:  metadata.Config.GetApiKey(),
		BaseURL: metadata.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return status.Error(codes.InvalidArgument, "OpenAI API key is required")
	}

	result, err := openai.UploadFileToVectorStore(ctx, cfg, metadata.StoreId, metadata.Filename, content)
	if err != nil {
		slog.Error("failed to upload to OpenAI vector store",
			"store_id", metadata.StoreId,
			"filename", metadata.Filename,
			"error", err,
		)
		return stream.SendAndClose(&pb.UploadFileResponse{
			FileId:   "",
			Filename: metadata.Filename,
			StoreId:  metadata.StoreId,
			Status:   "failed",
		})
	}

	slog.Info("file uploaded to OpenAI vector store",
		"store_id", metadata.StoreId,
		"filename", metadata.Filename,
		"file_id", result.FileID,
	)

	return stream.SendAndClose(&pb.UploadFileResponse{
		FileId:   result.FileID,
		Filename: result.Filename,
		StoreId:  result.StoreID,
		Status:   result.Status,
	})
}

// uploadToGemini uploads a file to a Gemini FileSearchStore.
func (s *FileService) uploadToGemini(ctx context.Context, stream pb.FileService_UploadFileServer, metadata *pb.UploadFileMetadata, content io.Reader) error {
	cfg := gemini.FileStoreConfig{
		APIKey:  metadata.Config.GetApiKey(),
		BaseURL: metadata.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return status.Error(codes.InvalidArgument, "Gemini API key is required")
	}

	result, err := gemini.UploadFileToFileSearchStore(ctx, cfg, metadata.StoreId, metadata.Filename, metadata.MimeType, content)
	if err != nil {
		slog.Error("failed to upload to Gemini file search store",
			"store_id", metadata.StoreId,
			"filename", metadata.Filename,
			"error", err,
		)
		return stream.SendAndClose(&pb.UploadFileResponse{
			FileId:   "",
			Filename: metadata.Filename,
			StoreId:  metadata.StoreId,
			Status:   "failed",
		})
	}

	slog.Info("file uploaded to Gemini file search store",
		"store_id", metadata.StoreId,
		"filename", metadata.Filename,
		"file_id", result.FileID,
	)

	return stream.SendAndClose(&pb.UploadFileResponse{
		FileId:   result.FileID,
		Filename: result.Filename,
		StoreId:  result.StoreID,
		Status:   result.Status,
	})
}

// uploadToInternal uploads a file to the internal Qdrant store.
func (s *FileService) uploadToInternal(ctx context.Context, stream pb.FileService_UploadFileServer, metadata *pb.UploadFileMetadata, content io.Reader) error {
	// Get tenant ID from auth context
	tenantID := auth.TenantIDFromContext(ctx)

	// Generate unique file ID
	fileID, err := generateFileID()
	if err != nil {
		return fmt.Errorf("generate file id: %w", err)
	}

	// Ingest the file via RAG service
	result, err := s.ragService.Ingest(ctx, rag.IngestParams{
		StoreID:  metadata.StoreId,
		TenantID: tenantID,
		File:     content,
		Filename: metadata.Filename,
		MIMEType: metadata.MimeType,
		FileID:   fileID,
	})
	if err != nil {
		slog.Error("failed to ingest file",
			"store_id", metadata.StoreId,
			"filename", metadata.Filename,
			"error", err,
		)
		return stream.SendAndClose(&pb.UploadFileResponse{
			FileId:   "",
			Filename: metadata.Filename,
			StoreId:  metadata.StoreId,
			Status:   "failed",
		})
	}

	slog.Info("file uploaded and indexed",
		"store_id", metadata.StoreId,
		"filename", metadata.Filename,
		"file_id", fileID,
		"chunks", result.ChunkCount,
	)

	return stream.SendAndClose(&pb.UploadFileResponse{
		FileId:   fileID,
		Filename: metadata.Filename,
		StoreId:  metadata.StoreId,
		Status:   "ready",
	})
}

// DeleteFileStore deletes a store and all its contents.
// Routes to appropriate backend based on provider.
func (s *FileService) DeleteFileStore(ctx context.Context, req *pb.DeleteFileStoreRequest) (*pb.DeleteFileStoreResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return nil, err
	}

	if req.StoreId == "" {
		return nil, fmt.Errorf("store_id is required")
	}

	// Route by provider
	switch req.Provider {
	case pb.Provider_PROVIDER_OPENAI:
		return s.deleteOpenAIVectorStore(ctx, req)
	case pb.Provider_PROVIDER_GEMINI:
		return s.deleteGeminiFileSearchStore(ctx, req)
	default:
		return s.deleteInternalStore(ctx, req)
	}
}

// deleteOpenAIVectorStore deletes an OpenAI Vector Store.
func (s *FileService) deleteOpenAIVectorStore(ctx context.Context, req *pb.DeleteFileStoreRequest) (*pb.DeleteFileStoreResponse, error) {
	cfg := openai.FileStoreConfig{
		APIKey:  req.Config.GetApiKey(),
		BaseURL: req.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return nil, status.Error(codes.InvalidArgument, "OpenAI API key is required")
	}

	if err := openai.DeleteVectorStore(ctx, cfg, req.StoreId); err != nil {
		slog.Error("failed to delete OpenAI vector store",
			"store_id", req.StoreId,
			"error", err,
		)
		return &pb.DeleteFileStoreResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	slog.Info("OpenAI vector store deleted", "store_id", req.StoreId)

	return &pb.DeleteFileStoreResponse{
		Success: true,
		Message: "store deleted successfully",
	}, nil
}

// deleteGeminiFileSearchStore deletes a Gemini FileSearchStore.
func (s *FileService) deleteGeminiFileSearchStore(ctx context.Context, req *pb.DeleteFileStoreRequest) (*pb.DeleteFileStoreResponse, error) {
	cfg := gemini.FileStoreConfig{
		APIKey:  req.Config.GetApiKey(),
		BaseURL: req.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return nil, status.Error(codes.InvalidArgument, "Gemini API key is required")
	}

	if err := gemini.DeleteFileSearchStore(ctx, cfg, req.StoreId, req.Force); err != nil {
		slog.Error("failed to delete Gemini file search store",
			"store_id", req.StoreId,
			"error", err,
		)
		return &pb.DeleteFileStoreResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	slog.Info("Gemini file search store deleted", "store_id", req.StoreId)

	return &pb.DeleteFileStoreResponse{
		Success: true,
		Message: "store deleted successfully",
	}, nil
}

// deleteInternalStore deletes an internal Qdrant store.
func (s *FileService) deleteInternalStore(ctx context.Context, req *pb.DeleteFileStoreRequest) (*pb.DeleteFileStoreResponse, error) {
	// Get tenant ID from auth context
	tenantID := auth.TenantIDFromContext(ctx)

	if err := s.ragService.DeleteStore(ctx, tenantID, req.StoreId); err != nil {
		slog.Error("failed to delete file store",
			"store_id", req.StoreId,
			"error", err,
		)
		return &pb.DeleteFileStoreResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	slog.Info("file store deleted", "store_id", req.StoreId)

	return &pb.DeleteFileStoreResponse{
		Success: true,
		Message: "store deleted successfully",
	}, nil
}

// GetFileStore retrieves store information.
// Routes to appropriate backend based on provider.
func (s *FileService) GetFileStore(ctx context.Context, req *pb.GetFileStoreRequest) (*pb.GetFileStoreResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return nil, err
	}

	if req.StoreId == "" {
		return nil, fmt.Errorf("store_id is required")
	}

	// Route by provider
	switch req.Provider {
	case pb.Provider_PROVIDER_OPENAI:
		return s.getOpenAIVectorStore(ctx, req)
	case pb.Provider_PROVIDER_GEMINI:
		return s.getGeminiFileSearchStore(ctx, req)
	default:
		return s.getInternalStore(ctx, req)
	}
}

// getOpenAIVectorStore retrieves an OpenAI Vector Store.
func (s *FileService) getOpenAIVectorStore(ctx context.Context, req *pb.GetFileStoreRequest) (*pb.GetFileStoreResponse, error) {
	cfg := openai.FileStoreConfig{
		APIKey:  req.Config.GetApiKey(),
		BaseURL: req.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return nil, status.Error(codes.InvalidArgument, "OpenAI API key is required")
	}

	result, err := openai.GetVectorStore(ctx, cfg, req.StoreId)
	if err != nil {
		return nil, fmt.Errorf("get OpenAI vector store: %w", err)
	}

	return &pb.GetFileStoreResponse{
		StoreId:   result.StoreID,
		Name:      result.Name,
		Provider:  pb.Provider_PROVIDER_OPENAI,
		FileCount: int32(result.FileCount),
		Status:    result.Status,
		CreatedAt: result.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// getGeminiFileSearchStore retrieves a Gemini FileSearchStore.
func (s *FileService) getGeminiFileSearchStore(ctx context.Context, req *pb.GetFileStoreRequest) (*pb.GetFileStoreResponse, error) {
	cfg := gemini.FileStoreConfig{
		APIKey:  req.Config.GetApiKey(),
		BaseURL: req.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return nil, status.Error(codes.InvalidArgument, "Gemini API key is required")
	}

	result, err := gemini.GetFileSearchStore(ctx, cfg, req.StoreId)
	if err != nil {
		return nil, fmt.Errorf("get Gemini file search store: %w", err)
	}

	return &pb.GetFileStoreResponse{
		StoreId:   result.StoreID,
		Name:      result.Name,
		Provider:  pb.Provider_PROVIDER_GEMINI,
		FileCount: int32(result.DocumentCount),
		Status:    result.Status,
		CreatedAt: result.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// getInternalStore retrieves an internal Qdrant store.
func (s *FileService) getInternalStore(ctx context.Context, req *pb.GetFileStoreRequest) (*pb.GetFileStoreResponse, error) {
	// Get tenant ID from auth context
	tenantID := auth.TenantIDFromContext(ctx)

	info, err := s.ragService.StoreInfo(ctx, tenantID, req.StoreId)
	if err != nil {
		return nil, fmt.Errorf("get store info: %w", err)
	}

	if info == nil {
		return nil, status.Error(codes.NotFound, "store not found")
	}

	return &pb.GetFileStoreResponse{
		StoreId:   req.StoreId,
		Name:      info.Name,
		Provider:  pb.Provider_PROVIDER_UNSPECIFIED,
		FileCount: int32(info.PointCount),
		Status:    "ready",
		CreatedAt: "",
	}, nil
}

// ListFileStores lists all stores for a client.
// Routes to appropriate backend based on provider.
func (s *FileService) ListFileStores(ctx context.Context, req *pb.ListFileStoresRequest) (*pb.ListFileStoresResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return nil, err
	}

	// Route by provider
	switch req.Provider {
	case pb.Provider_PROVIDER_OPENAI:
		return s.listOpenAIVectorStores(ctx, req)
	case pb.Provider_PROVIDER_GEMINI:
		return s.listGeminiFileSearchStores(ctx, req)
	default:
		return nil, status.Error(codes.Unimplemented, "ListFileStores not yet implemented for internal stores")
	}
}

// listOpenAIVectorStores lists OpenAI Vector Stores.
func (s *FileService) listOpenAIVectorStores(ctx context.Context, req *pb.ListFileStoresRequest) (*pb.ListFileStoresResponse, error) {
	cfg := openai.FileStoreConfig{
		APIKey:  req.Config.GetApiKey(),
		BaseURL: req.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return nil, status.Error(codes.InvalidArgument, "OpenAI API key is required")
	}

	results, err := openai.ListVectorStores(ctx, cfg, int(req.Limit))
	if err != nil {
		return nil, fmt.Errorf("list OpenAI vector stores: %w", err)
	}

	var stores []*pb.FileStoreSummary
	for _, r := range results {
		stores = append(stores, &pb.FileStoreSummary{
			StoreId:   r.StoreID,
			Name:      r.Name,
			Provider:  pb.Provider_PROVIDER_OPENAI,
			FileCount: int32(r.FileCount),
			Status:    r.Status,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	return &pb.ListFileStoresResponse{
		Stores: stores,
	}, nil
}

// listGeminiFileSearchStores lists Gemini FileSearchStores.
func (s *FileService) listGeminiFileSearchStores(ctx context.Context, req *pb.ListFileStoresRequest) (*pb.ListFileStoresResponse, error) {
	cfg := gemini.FileStoreConfig{
		APIKey:  req.Config.GetApiKey(),
		BaseURL: req.Config.GetBaseUrl(),
	}

	if cfg.APIKey == "" {
		return nil, status.Error(codes.InvalidArgument, "Gemini API key is required")
	}

	results, err := gemini.ListFileSearchStores(ctx, cfg, int(req.Limit))
	if err != nil {
		return nil, fmt.Errorf("list Gemini file search stores: %w", err)
	}

	var stores []*pb.FileStoreSummary
	for _, r := range results {
		stores = append(stores, &pb.FileStoreSummary{
			StoreId:   r.StoreID,
			Name:      r.Name,
			Provider:  pb.Provider_PROVIDER_GEMINI,
			FileCount: int32(r.DocumentCount),
			Status:    r.Status,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	return &pb.ListFileStoresResponse{
		Stores: stores,
	}, nil
}
