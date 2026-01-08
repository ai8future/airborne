package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"time"

	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
	"github.com/cliffpyles/aibox/internal/auth"
	"github.com/cliffpyles/aibox/internal/rag"
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

	ragService *rag.Service
}

// NewFileService creates a new file service.
func NewFileService(ragService *rag.Service) *FileService {
	return &FileService{
		ragService: ragService,
	}
}

// CreateFileStore creates a new vector store (Qdrant collection).
func (s *FileService) CreateFileStore(ctx context.Context, req *pb.CreateFileStoreRequest) (*pb.CreateFileStoreResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return nil, err
	}

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
		Provider:  pb.Provider_PROVIDER_UNSPECIFIED, // We use self-hosted Qdrant
		Name:      req.Name,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// UploadFile uploads a file to a store using client streaming.
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
	)

	// Collect file chunks with size limit enforcement
	var buf bytes.Buffer
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

		buf.Write(chunk)
	}

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
		File:     &buf,
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
func (s *FileService) DeleteFileStore(ctx context.Context, req *pb.DeleteFileStoreRequest) (*pb.DeleteFileStoreResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return nil, err
	}

	if req.StoreId == "" {
		return nil, fmt.Errorf("store_id is required")
	}

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
func (s *FileService) GetFileStore(ctx context.Context, req *pb.GetFileStoreRequest) (*pb.GetFileStoreResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return nil, err
	}

	if req.StoreId == "" {
		return nil, fmt.Errorf("store_id is required")
	}

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
		FileCount: int32(info.PointCount), // Each file may have multiple chunks
		Status:    "ready",
		CreatedAt: "", // Not tracked in Qdrant by default
	}, nil
}

// ListFileStores lists all stores for a client.
func (s *FileService) ListFileStores(ctx context.Context, req *pb.ListFileStoresRequest) (*pb.ListFileStoresResponse, error) {
	// Check permission
	if err := auth.RequirePermission(ctx, auth.PermissionFiles); err != nil {
		return nil, err
	}

	return nil, status.Error(codes.Unimplemented, "ListFileStores not yet implemented")
}
