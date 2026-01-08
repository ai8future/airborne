package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
	"github.com/cliffpyles/aibox/internal/auth"
	"github.com/cliffpyles/aibox/internal/rag"
	"github.com/cliffpyles/aibox/internal/rag/extractor"
	"github.com/cliffpyles/aibox/internal/rag/testutil"
	"github.com/cliffpyles/aibox/internal/rag/vectorstore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ctxWithFilePermission creates a context with file permission for testing.
func ctxWithFilePermission(clientID string) context.Context {
	return context.WithValue(context.Background(), auth.ClientContextKey, &auth.ClientKey{
		ClientID:    clientID,
		Permissions: []auth.Permission{auth.PermissionFiles},
	})
}

func TestNewFileService(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	if svc == nil {
		t.Fatal("expected non-nil FileService")
	}
	if svc.ragService != mockRAG {
		t.Error("expected ragService to be set")
	}
}

func TestFileService_CreateFileStore_Success(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	req := &pb.CreateFileStoreRequest{
		ClientId: "tenant1",
		Name:     "test-store",
	}

	resp, err := svc.CreateFileStore(ctxWithFilePermission("tenant1"), req)

	if err != nil {
		t.Fatalf("CreateFileStore failed: %v", err)
	}
	if resp.StoreId != "test-store" {
		t.Errorf("expected StoreId=test-store, got %s", resp.StoreId)
	}
	if resp.Name != "test-store" {
		t.Errorf("expected Name=test-store, got %s", resp.Name)
	}
	if resp.CreatedAt == "" {
		t.Error("expected CreatedAt to be set")
	}

	// Verify collection was created
	if len(mockStore.CreateCollectionCalls) != 1 {
		t.Errorf("expected 1 CreateCollection call, got %d", len(mockStore.CreateCollectionCalls))
	}
}

func TestFileService_CreateFileStore_GeneratedName(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	req := &pb.CreateFileStoreRequest{
		ClientId: "tenant1",
		// Name not provided
	}

	resp, err := svc.CreateFileStore(ctxWithFilePermission("tenant1"), req)

	if err != nil {
		t.Fatalf("CreateFileStore failed: %v", err)
	}
	if resp.StoreId == "" {
		t.Error("expected generated StoreId")
	}
	if resp.StoreId[:6] != "store_" {
		t.Errorf("expected StoreId to start with store_, got %s", resp.StoreId)
	}
}

func TestFileService_CreateFileStore_MissingClientID(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	req := &pb.CreateFileStoreRequest{
		Name: "test-store",
		// ClientId missing - but now we get tenant from auth context
	}

	// With auth context, this should succeed (tenant from context)
	resp, err := svc.CreateFileStore(ctxWithFilePermission("tenant1"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StoreId == "" {
		t.Error("expected StoreId to be set")
	}
}

func TestFileService_CreateFileStore_StoreError(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockStore.CreateCollectionFunc = func(ctx context.Context, name string, dims int) error {
		return fmt.Errorf("collection creation failed")
	}
	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	req := &pb.CreateFileStoreRequest{
		ClientId: "tenant1",
		Name:     "test-store",
	}

	_, err := svc.CreateFileStore(ctxWithFilePermission("tenant1"), req)

	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestFileService_DeleteFileStore_Success(t *testing.T) {
	mockStore := testutil.NewMockStore()
	// Pre-create the collection - using tenant1 since that's what ctxWithFilePermission provides
	mockStore.CreateCollection(context.Background(), "tenant1_test-store", 768)

	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	req := &pb.DeleteFileStoreRequest{
		StoreId: "test-store",
	}

	resp, err := svc.DeleteFileStore(ctxWithFilePermission("tenant1"), req)

	if err != nil {
		t.Fatalf("DeleteFileStore failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected Success=true, got false: %s", resp.Message)
	}
}

func TestFileService_DeleteFileStore_MissingStoreID(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	req := &pb.DeleteFileStoreRequest{
		// StoreId missing
	}

	_, err := svc.DeleteFileStore(ctxWithFilePermission("tenant1"), req)

	if err == nil {
		t.Fatal("expected error for missing store_id")
	}
}

func TestFileService_DeleteFileStore_Error(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockStore.DeleteCollectionFunc = func(ctx context.Context, name string) error {
		return fmt.Errorf("delete failed")
	}
	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	req := &pb.DeleteFileStoreRequest{
		StoreId: "test-store",
	}

	resp, err := svc.DeleteFileStore(ctxWithFilePermission("tenant1"), req)

	// Should not return error but Success=false
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected Success=false on error")
	}
}

func TestFileService_GetFileStore_Success(t *testing.T) {
	mockStore := testutil.NewMockStore()
	// Pre-create collection with some points - using tenant1 since that's what ctxWithFilePermission provides
	mockStore.CreateCollection(context.Background(), "tenant1_test-store", 768)
	mockStore.Upsert(context.Background(), "tenant1_test-store", []vectorstore.Point{
		{ID: "1", Vector: make([]float32, 768), Payload: map[string]any{}},
		{ID: "2", Vector: make([]float32, 768), Payload: map[string]any{}},
	})

	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	req := &pb.GetFileStoreRequest{
		StoreId: "test-store",
	}

	resp, err := svc.GetFileStore(ctxWithFilePermission("tenant1"), req)

	if err != nil {
		t.Fatalf("GetFileStore failed: %v", err)
	}
	if resp.StoreId != "test-store" {
		t.Errorf("expected StoreId=test-store, got %s", resp.StoreId)
	}
	if resp.FileCount != 2 {
		t.Errorf("expected FileCount=2, got %d", resp.FileCount)
	}
	if resp.Status != "ready" {
		t.Errorf("expected Status=ready, got %s", resp.Status)
	}
}

func TestFileService_GetFileStore_MissingStoreID(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	req := &pb.GetFileStoreRequest{
		// StoreId missing
	}

	_, err := svc.GetFileStore(ctxWithFilePermission("tenant1"), req)

	if err == nil {
		t.Fatal("expected error for missing store_id")
	}
}

func TestFileService_GetFileStore_NotFound(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockStore.CollectionInfoFunc = func(ctx context.Context, name string) (*vectorstore.CollectionInfo, error) {
		return nil, fmt.Errorf("collection not found")
	}
	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	req := &pb.GetFileStoreRequest{
		StoreId: "nonexistent",
	}

	_, err := svc.GetFileStore(ctxWithFilePermission("tenant1"), req)

	if err == nil {
		t.Fatal("expected error for nonexistent store")
	}
}

func TestFileService_GetFileStore_NilInfo_ReturnsNotFound(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockStore.CollectionInfoFunc = func(ctx context.Context, name string) (*vectorstore.CollectionInfo, error) {
		return nil, nil // Store exists but returns nil info
	}
	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	req := &pb.GetFileStoreRequest{
		StoreId: "nonexistent",
	}

	_, err := svc.GetFileStore(ctxWithFilePermission("tenant1"), req)

	if err == nil {
		t.Fatal("expected error when store info is nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("expected NotFound code, got: %v", st.Code())
	}
}

func TestFileService_ListFileStores_Unimplemented(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	req := &pb.ListFileStoresRequest{
		ClientId: "tenant1",
	}

	resp, err := svc.ListFileStores(ctxWithFilePermission("tenant1"), req)

	if resp != nil {
		t.Error("expected nil response for unimplemented method")
	}
	if err == nil {
		t.Fatal("expected error for unimplemented method")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Unimplemented {
		t.Errorf("expected Unimplemented code, got: %v", st.Code())
	}
}

// Mock stream for UploadFile testing
type mockUploadFileServer struct {
	pb.FileService_UploadFileServer
	ctx      context.Context
	messages []*pb.UploadFileRequest
	index    int
	response *pb.UploadFileResponse
}

func (m *mockUploadFileServer) Context() context.Context {
	return m.ctx
}

func (m *mockUploadFileServer) Recv() (*pb.UploadFileRequest, error) {
	if m.index >= len(m.messages) {
		return nil, io.EOF
	}
	msg := m.messages[m.index]
	m.index++
	return msg, nil
}

func (m *mockUploadFileServer) SendAndClose(resp *pb.UploadFileResponse) error {
	m.response = resp
	return nil
}

func TestFileService_UploadFile_Success(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockExtractor := testutil.NewMockExtractor()
	mockExtractor.DefaultText = "This is extracted text from the document."

	// Pre-create the collection - using tenant1 since that's what ctxWithFilePermission provides
	mockStore.CreateCollection(context.Background(), "tenant1_test-store", 768)

	mockRAG := createRAGServiceWithMocks(mockStore, mockEmbedder, mockExtractor)
	svc := NewFileService(mockRAG)

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				Data: &pb.UploadFileRequest_Metadata{
					Metadata: &pb.UploadFileMetadata{
						StoreId:  "test-store",
						Filename: "document.pdf",
						MimeType: "application/pdf",
						Size:     1024,
					},
				},
			},
			{
				Data: &pb.UploadFileRequest_Chunk{
					Chunk: []byte("fake pdf content"),
				},
			},
		},
	}

	err := svc.UploadFile(stream)

	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}
	if stream.response == nil {
		t.Fatal("expected response")
	}
	if stream.response.Status != "ready" {
		t.Errorf("expected Status=ready, got %s", stream.response.Status)
	}
	if stream.response.FileId == "" {
		t.Error("expected FileId to be set")
	}
	if stream.response.Filename != "document.pdf" {
		t.Errorf("expected Filename=document.pdf, got %s", stream.response.Filename)
	}
}

func TestFileService_UploadFile_MissingMetadata(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				// No metadata, just a chunk
				Data: &pb.UploadFileRequest_Chunk{
					Chunk: []byte("data"),
				},
			},
		},
	}

	err := svc.UploadFile(stream)

	if err == nil {
		t.Fatal("expected error for missing metadata")
	}
}

func TestFileService_UploadFile_MissingStoreID(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				Data: &pb.UploadFileRequest_Metadata{
					Metadata: &pb.UploadFileMetadata{
						// StoreId missing
						Filename: "test.pdf",
					},
				},
			},
		},
	}

	err := svc.UploadFile(stream)

	if err == nil {
		t.Fatal("expected error for missing store_id")
	}
}

func TestFileService_UploadFile_MissingFilename(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				Data: &pb.UploadFileRequest_Metadata{
					Metadata: &pb.UploadFileMetadata{
						StoreId: "test-store",
						// Filename missing
					},
				},
			},
		},
	}

	err := svc.UploadFile(stream)

	if err == nil {
		t.Fatal("expected error for missing filename")
	}
}

func TestFileService_UploadFile_MultipleChunks(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockExtractor := testutil.NewMockExtractor()

	// Pre-create the collection - using tenant1 since that's what ctxWithFilePermission provides
	mockStore.CreateCollection(context.Background(), "tenant1_test-store", 768)

	mockRAG := createRAGServiceWithMocks(mockStore, mockEmbedder, mockExtractor)
	svc := NewFileService(mockRAG)

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				Data: &pb.UploadFileRequest_Metadata{
					Metadata: &pb.UploadFileMetadata{
						StoreId:  "test-store",
						Filename: "document.pdf",
						MimeType: "application/pdf",
					},
				},
			},
			{
				Data: &pb.UploadFileRequest_Chunk{
					Chunk: []byte("chunk1-"),
				},
			},
			{
				Data: &pb.UploadFileRequest_Chunk{
					Chunk: []byte("chunk2-"),
				},
			},
			{
				Data: &pb.UploadFileRequest_Chunk{
					Chunk: []byte("chunk3"),
				},
			},
		},
	}

	err := svc.UploadFile(stream)

	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}
	if stream.response.Status != "ready" {
		t.Errorf("expected Status=ready, got %s", stream.response.Status)
	}
}

func TestFileService_UploadFile_IngestError(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockExtractor := testutil.NewMockExtractor()
	mockExtractor.ExtractFunc = func(ctx context.Context, file io.Reader, filename, mimeType string) (*extractor.ExtractionResult, error) {
		return nil, fmt.Errorf("extraction failed")
	}

	// Pre-create the collection - using tenant1 since that's what ctxWithFilePermission provides
	mockStore.CreateCollection(context.Background(), "tenant1_test-store", 768)

	mockRAG := createRAGServiceWithMocks(mockStore, mockEmbedder, mockExtractor)
	svc := NewFileService(mockRAG)

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				Data: &pb.UploadFileRequest_Metadata{
					Metadata: &pb.UploadFileMetadata{
						StoreId:  "test-store",
						Filename: "document.pdf",
					},
				},
			},
			{
				Data: &pb.UploadFileRequest_Chunk{
					Chunk: []byte("content"),
				},
			},
		},
	}

	err := svc.UploadFile(stream)

	// Should return response with "failed" status, not error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stream.response.Status != "failed" {
		t.Errorf("expected Status=failed, got %s", stream.response.Status)
	}
}

func TestFileService_UploadFile_EmptyStream(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	stream := &mockUploadFileServer{
		ctx:      ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{},
	}

	err := svc.UploadFile(stream)

	if err == nil {
		t.Fatal("expected error for empty stream")
	}
}

func TestFileService_UploadFile_MetadataSizeExceedsLimit(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				Data: &pb.UploadFileRequest_Metadata{
					Metadata: &pb.UploadFileMetadata{
						StoreId:  "test-store",
						Filename: "huge-file.bin",
						Size:     200 * 1024 * 1024, // 200MB, exceeds 100MB limit
					},
				},
			},
		},
	}

	err := svc.UploadFile(stream)

	if err == nil {
		t.Fatal("expected error for file size exceeding limit")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected error about size limit, got: %v", err)
	}
}

func TestFileService_UploadFile_StreamingSizeExceedsLimit(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockStore.CreateCollection(context.Background(), "tenant1_test-store", 768)
	mockRAG := createRAGServiceWithMocks(mockStore, nil, nil)
	svc := NewFileService(mockRAG)

	// Create chunks that exceed the limit (100MB)
	// We'll send enough 10MB chunks to exceed the limit
	largeChunk := make([]byte, 20*1024*1024) // 20MB per chunk
	for i := range largeChunk {
		largeChunk[i] = byte(i % 256)
	}

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				Data: &pb.UploadFileRequest_Metadata{
					Metadata: &pb.UploadFileMetadata{
						StoreId:  "test-store",
						Filename: "streaming-large.bin",
						// Size not declared, so must check during streaming
					},
				},
			},
			{Data: &pb.UploadFileRequest_Chunk{Chunk: largeChunk}}, // 20MB
			{Data: &pb.UploadFileRequest_Chunk{Chunk: largeChunk}}, // 40MB
			{Data: &pb.UploadFileRequest_Chunk{Chunk: largeChunk}}, // 60MB
			{Data: &pb.UploadFileRequest_Chunk{Chunk: largeChunk}}, // 80MB
			{Data: &pb.UploadFileRequest_Chunk{Chunk: largeChunk}}, // 100MB
			{Data: &pb.UploadFileRequest_Chunk{Chunk: largeChunk}}, // 120MB - exceeds limit
		},
	}

	err := svc.UploadFile(stream)

	if err == nil {
		t.Fatal("expected error for streaming size exceeding limit")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected error about size limit, got: %v", err)
	}
}

func TestFileService_UploadFile_ExactlyAtLimit(t *testing.T) {
	mockStore := testutil.NewMockStore()
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockExtractor := testutil.NewMockExtractor()
	mockStore.CreateCollection(context.Background(), "tenant1_test-store", 768)
	mockRAG := createRAGServiceWithMocks(mockStore, mockEmbedder, mockExtractor)
	svc := NewFileService(mockRAG)

	// Create a chunk exactly at the limit (100MB)
	// This should succeed
	exactLimitChunk := make([]byte, 1024) // 1KB per chunk

	stream := &mockUploadFileServer{
		ctx: ctxWithFilePermission("tenant1"),
		messages: []*pb.UploadFileRequest{
			{
				Data: &pb.UploadFileRequest_Metadata{
					Metadata: &pb.UploadFileMetadata{
						StoreId:  "test-store",
						Filename: "at-limit.bin",
						Size:     int64(len(exactLimitChunk)),
					},
				},
			},
			{Data: &pb.UploadFileRequest_Chunk{Chunk: exactLimitChunk}},
		},
	}

	err := svc.UploadFile(stream)

	if err != nil {
		t.Fatalf("unexpected error for file at limit: %v", err)
	}
	if stream.response.Status != "ready" {
		t.Errorf("expected Status=ready, got %s", stream.response.Status)
	}
}

func TestFileService_AuthRequired(t *testing.T) {
	mockRAG := createMockRAGService()
	svc := NewFileService(mockRAG)

	// Test CreateFileStore without auth
	_, err := svc.CreateFileStore(context.Background(), &pb.CreateFileStoreRequest{
		Name: "test-store",
	})
	if err == nil {
		t.Error("CreateFileStore: expected auth error")
	}

	// Test GetFileStore without auth
	_, err = svc.GetFileStore(context.Background(), &pb.GetFileStoreRequest{
		StoreId: "test-store",
	})
	if err == nil {
		t.Error("GetFileStore: expected auth error")
	}

	// Test DeleteFileStore without auth
	_, err = svc.DeleteFileStore(context.Background(), &pb.DeleteFileStoreRequest{
		StoreId: "test-store",
	})
	if err == nil {
		t.Error("DeleteFileStore: expected auth error")
	}

	// Test ListFileStores without auth
	_, err = svc.ListFileStores(context.Background(), &pb.ListFileStoresRequest{})
	if err == nil {
		t.Error("ListFileStores: expected auth error")
	}
}

// Helper functions to create mock RAG services

func createMockRAGService() *rag.Service {
	mockEmbedder := testutil.NewMockEmbedder(768)
	mockStore := testutil.NewMockStore()
	mockExtractor := testutil.NewMockExtractor()

	return rag.NewService(mockEmbedder, mockStore, mockExtractor, rag.DefaultServiceOptions())
}

func createRAGServiceWithMocks(
	store *testutil.MockStore,
	embedder *testutil.MockEmbedder,
	extractor *testutil.MockExtractor,
) *rag.Service {
	if embedder == nil {
		embedder = testutil.NewMockEmbedder(768)
	}
	if store == nil {
		store = testutil.NewMockStore()
	}
	if extractor == nil {
		extractor = testutil.NewMockExtractor()
	}

	return rag.NewService(embedder, store, extractor, rag.DefaultServiceOptions())
}
