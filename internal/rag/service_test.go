package rag

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cliffpyles/aibox/internal/rag/extractor"
	"github.com/cliffpyles/aibox/internal/rag/testutil"
	"github.com/cliffpyles/aibox/internal/rag/vectorstore"
)

func newTestService(t *testing.T) (*Service, *testutil.MockEmbedder, *testutil.MockStore, *testutil.MockExtractor) {
	t.Helper()
	mockEmb := testutil.NewMockEmbedder(768)
	mockStore := testutil.NewMockStore()
	mockExt := testutil.NewMockExtractor()

	svc := NewService(mockEmb, mockStore, mockExt, DefaultServiceOptions())
	return svc, mockEmb, mockStore, mockExt
}

func TestNewService_Defaults(t *testing.T) {
	mockEmb := testutil.NewMockEmbedder(768)
	mockStore := testutil.NewMockStore()
	mockExt := testutil.NewMockExtractor()

	// Zero options should use defaults
	svc := NewService(mockEmb, mockStore, mockExt, ServiceOptions{})

	if svc.opts.ChunkSize != 2000 {
		t.Errorf("expected default ChunkSize=2000, got %d", svc.opts.ChunkSize)
	}
	if svc.opts.ChunkOverlap != 200 {
		t.Errorf("expected default ChunkOverlap=200, got %d", svc.opts.ChunkOverlap)
	}
	if svc.opts.RetrievalTopK != 5 {
		t.Errorf("expected default RetrievalTopK=5, got %d", svc.opts.RetrievalTopK)
	}
}

func TestService_Ingest_Success(t *testing.T) {
	svc, mockEmb, mockStore, mockExt := newTestService(t)
	ctx := context.Background()

	// Set up extractor to return substantial text
	mockExt.DefaultText = strings.Repeat("This is test content. ", 200)

	result, err := svc.Ingest(ctx, IngestParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		ThreadID: "thread1",
		File:     bytes.NewReader([]byte("fake pdf content")),
		Filename: "test.pdf",
		MIMEType: "application/pdf",
	})

	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	if result.ChunkCount == 0 {
		t.Error("expected chunks to be created")
	}

	// Verify extractor was called
	if len(mockExt.ExtractCalls) != 1 {
		t.Errorf("expected 1 extract call, got %d", len(mockExt.ExtractCalls))
	}
	if mockExt.ExtractCalls[0].Filename != "test.pdf" {
		t.Errorf("expected filename=test.pdf, got %s", mockExt.ExtractCalls[0].Filename)
	}

	// Verify embedder was called
	if len(mockEmb.EmbedBatchCalls) != 1 {
		t.Errorf("expected 1 embedBatch call, got %d", len(mockEmb.EmbedBatchCalls))
	}

	// Verify store was called
	if len(mockStore.UpsertCalls) != 1 {
		t.Errorf("expected 1 upsert call, got %d", len(mockStore.UpsertCalls))
	}

	// Verify collection was created
	if len(mockStore.CreateCollectionCalls) != 1 {
		t.Errorf("expected 1 createCollection call, got %d", len(mockStore.CreateCollectionCalls))
	}
}

func TestService_Ingest_CreatesCollection(t *testing.T) {
	svc, _, mockStore, mockExt := newTestService(t)
	ctx := context.Background()

	mockExt.DefaultText = "Some text content."

	_, err := svc.Ingest(ctx, IngestParams{
		StoreID:  "newstore",
		TenantID: "tenant1",
		File:     bytes.NewReader([]byte("content")),
		Filename: "doc.txt",
	})

	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	// Collection should be created
	if len(mockStore.CreateCollectionCalls) != 1 {
		t.Errorf("expected collection to be created")
	}

	expectedName := "tenant1_newstore"
	if mockStore.CreateCollectionCalls[0].Name != expectedName {
		t.Errorf("expected collection name=%s, got %s",
			expectedName, mockStore.CreateCollectionCalls[0].Name)
	}
}

func TestService_Ingest_ExistingCollection(t *testing.T) {
	svc, _, mockStore, mockExt := newTestService(t)
	ctx := context.Background()

	// Pre-create the collection
	mockStore.CreateCollection(ctx, "tenant1_store1", 768)
	mockStore.CreateCollectionCalls = nil // Reset tracking

	mockExt.DefaultText = "Some text."

	_, err := svc.Ingest(ctx, IngestParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		File:     bytes.NewReader([]byte("content")),
		Filename: "doc.txt",
	})

	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	// Should not try to create again
	if len(mockStore.CreateCollectionCalls) != 0 {
		t.Error("should not create collection if it exists")
	}
}

func TestService_Ingest_ExtractorError(t *testing.T) {
	svc, mockEmb, mockStore, mockExt := newTestService(t)
	ctx := context.Background()

	expectedErr := errors.New("extraction failed")
	mockExt.ExtractFunc = func(ctx context.Context, file io.Reader, filename, mimeType string) (*extractor.ExtractionResult, error) {
		return nil, expectedErr
	}

	_, err := svc.Ingest(ctx, IngestParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		File:     bytes.NewReader([]byte("content")),
		Filename: "doc.pdf",
	})

	if err == nil {
		t.Fatal("expected error from extractor")
	}
	if !strings.Contains(err.Error(), "extract text") {
		t.Errorf("error should mention extraction: %v", err)
	}

	// Embedder and store should not be called
	if len(mockEmb.EmbedBatchCalls) > 0 {
		t.Error("embedder should not be called on extraction error")
	}
	if len(mockStore.UpsertCalls) > 0 {
		t.Error("store should not be called on extraction error")
	}
}

func TestService_Ingest_EmbedderError(t *testing.T) {
	svc, mockEmb, mockStore, mockExt := newTestService(t)
	ctx := context.Background()

	mockExt.DefaultText = "Some text content."

	expectedErr := errors.New("embedding failed")
	mockEmb.EmbedBatchFunc = func(ctx context.Context, texts []string) ([][]float32, error) {
		return nil, expectedErr
	}

	_, err := svc.Ingest(ctx, IngestParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		File:     bytes.NewReader([]byte("content")),
		Filename: "doc.txt",
	})

	if err == nil {
		t.Fatal("expected error from embedder")
	}
	if !strings.Contains(err.Error(), "embeddings") {
		t.Errorf("error should mention embeddings: %v", err)
	}

	// Store should not be called
	if len(mockStore.UpsertCalls) > 0 {
		t.Error("store should not be called on embedding error")
	}
}

func TestService_Ingest_StoreError(t *testing.T) {
	svc, _, mockStore, mockExt := newTestService(t)
	ctx := context.Background()

	mockExt.DefaultText = "Some text content."

	expectedErr := errors.New("store failed")
	mockStore.UpsertFunc = func(ctx context.Context, collection string, points []vectorstore.Point) error {
		return expectedErr
	}

	_, err := svc.Ingest(ctx, IngestParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		File:     bytes.NewReader([]byte("content")),
		Filename: "doc.txt",
	})

	if err == nil {
		t.Fatal("expected error from store")
	}
	if !strings.Contains(err.Error(), "store embeddings") {
		t.Errorf("error should mention storing: %v", err)
	}
}

func TestService_Ingest_EmptyText(t *testing.T) {
	svc, mockEmb, mockStore, mockExt := newTestService(t)
	ctx := context.Background()

	mockExt.DefaultText = ""

	result, err := svc.Ingest(ctx, IngestParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		File:     bytes.NewReader([]byte("empty doc")),
		Filename: "empty.txt",
	})

	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	if result.ChunkCount != 0 {
		t.Errorf("expected 0 chunks for empty text, got %d", result.ChunkCount)
	}

	// Embedder and upsert should not be called
	if len(mockEmb.EmbedBatchCalls) > 0 {
		t.Error("embedder should not be called for empty text")
	}
	if len(mockStore.UpsertCalls) > 0 {
		t.Error("store should not be called for empty text")
	}
}

func TestService_Ingest_PointMetadata(t *testing.T) {
	svc, _, mockStore, mockExt := newTestService(t)
	ctx := context.Background()

	mockExt.DefaultText = "Test content for metadata verification."

	_, err := svc.Ingest(ctx, IngestParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		ThreadID: "thread1",
		File:     bytes.NewReader([]byte("content")),
		Filename: "test.pdf",
		MIMEType: "application/pdf",
	})

	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	if len(mockStore.UpsertCalls) == 0 {
		t.Fatal("expected upsert call")
	}

	points := mockStore.UpsertCalls[0].Points
	if len(points) == 0 {
		t.Fatal("expected points to be upserted")
	}

	p := points[0]

	// Check metadata fields
	if p.Payload["tenant_id"] != "tenant1" {
		t.Errorf("expected tenant_id=tenant1, got %v", p.Payload["tenant_id"])
	}
	if p.Payload["thread_id"] != "thread1" {
		t.Errorf("expected thread_id=thread1, got %v", p.Payload["thread_id"])
	}
	if p.Payload["store_id"] != "store1" {
		t.Errorf("expected store_id=store1, got %v", p.Payload["store_id"])
	}
	if p.Payload["filename"] != "test.pdf" {
		t.Errorf("expected filename=test.pdf, got %v", p.Payload["filename"])
	}
	if _, ok := p.Payload["text"]; !ok {
		t.Error("expected text in payload")
	}
	if _, ok := p.Payload["chunk_index"]; !ok {
		t.Error("expected chunk_index in payload")
	}
}

func TestService_Retrieve_Success(t *testing.T) {
	svc, mockEmb, mockStore, _ := newTestService(t)
	ctx := context.Background()

	// Set up collection with data
	collName := "tenant1_store1"
	mockStore.CreateCollection(ctx, collName, 768)
	mockStore.Upsert(ctx, collName, []vectorstore.Point{
		{ID: "1", Vector: testutil.RandomEmbedding(768), Payload: map[string]any{
			"text": "First chunk content", "filename": "doc.pdf", "chunk_index": 0,
		}},
		{ID: "2", Vector: testutil.RandomEmbedding(768), Payload: map[string]any{
			"text": "Second chunk content", "filename": "doc.pdf", "chunk_index": 1,
		}},
	})
	mockStore.SearchCalls = nil // Reset

	results, err := svc.Retrieve(ctx, RetrieveParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		Query:    "what is in the document?",
		TopK:     5,
	})

	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected results")
	}

	// Verify embedder was called with query
	if len(mockEmb.EmbedCalls) != 1 {
		t.Errorf("expected 1 embed call, got %d", len(mockEmb.EmbedCalls))
	}
	if mockEmb.EmbedCalls[0] != "what is in the document?" {
		t.Errorf("wrong query embedded: %s", mockEmb.EmbedCalls[0])
	}

	// Verify store was searched
	if len(mockStore.SearchCalls) != 1 {
		t.Errorf("expected 1 search call, got %d", len(mockStore.SearchCalls))
	}
}

func TestService_Retrieve_NoCollection(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()

	results, err := svc.Retrieve(ctx, RetrieveParams{
		StoreID:  "nonexistent",
		TenantID: "tenant1",
		Query:    "query",
	})

	if err != nil {
		t.Fatalf("Retrieve should not error for missing collection: %v", err)
	}

	if results != nil {
		t.Errorf("expected nil results for missing collection, got %v", results)
	}
}

func TestService_Retrieve_EmptyResults(t *testing.T) {
	svc, _, mockStore, _ := newTestService(t)
	ctx := context.Background()

	// Create empty collection
	mockStore.CreateCollection(ctx, "tenant1_store1", 768)

	results, err := svc.Retrieve(ctx, RetrieveParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		Query:    "query",
	})

	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestService_Retrieve_WithThreadFilter(t *testing.T) {
	svc, _, mockStore, _ := newTestService(t)
	ctx := context.Background()

	mockStore.CreateCollection(ctx, "tenant1_store1", 768)

	_, err := svc.Retrieve(ctx, RetrieveParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		Query:    "query",
		ThreadID: "thread123",
	})

	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(mockStore.SearchCalls) != 1 {
		t.Fatal("expected search call")
	}

	params := mockStore.SearchCalls[0]
	if params.Filter == nil {
		t.Fatal("expected filter for thread_id")
	}
	if len(params.Filter.Must) != 1 {
		t.Errorf("expected 1 filter condition, got %d", len(params.Filter.Must))
	}
	if params.Filter.Must[0].Field != "thread_id" {
		t.Errorf("expected filter on thread_id, got %s", params.Filter.Must[0].Field)
	}
	if params.Filter.Must[0].Match != "thread123" {
		t.Errorf("expected filter value=thread123, got %v", params.Filter.Must[0].Match)
	}
}

func TestService_Retrieve_TopK(t *testing.T) {
	svc, _, mockStore, _ := newTestService(t)
	ctx := context.Background()

	mockStore.CreateCollection(ctx, "tenant1_store1", 768)

	_, err := svc.Retrieve(ctx, RetrieveParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		Query:    "query",
		TopK:     10,
	})

	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(mockStore.SearchCalls) != 1 {
		t.Fatal("expected search call")
	}

	if mockStore.SearchCalls[0].Limit != 10 {
		t.Errorf("expected limit=10, got %d", mockStore.SearchCalls[0].Limit)
	}
}

func TestService_Retrieve_DefaultTopK(t *testing.T) {
	svc, _, mockStore, _ := newTestService(t)
	ctx := context.Background()

	mockStore.CreateCollection(ctx, "tenant1_store1", 768)

	_, err := svc.Retrieve(ctx, RetrieveParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		Query:    "query",
		// TopK not set
	})

	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(mockStore.SearchCalls) != 1 {
		t.Fatal("expected search call")
	}

	// Should use default from service options (5)
	if mockStore.SearchCalls[0].Limit != 5 {
		t.Errorf("expected default limit=5, got %d", mockStore.SearchCalls[0].Limit)
	}
}

func TestService_Retrieve_EmbedderError(t *testing.T) {
	svc, mockEmb, mockStore, _ := newTestService(t)
	ctx := context.Background()

	mockStore.CreateCollection(ctx, "tenant1_store1", 768)

	expectedErr := errors.New("embed failed")
	mockEmb.EmbedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, expectedErr
	}

	_, err := svc.Retrieve(ctx, RetrieveParams{
		StoreID:  "store1",
		TenantID: "tenant1",
		Query:    "query",
	})

	if err == nil {
		t.Fatal("expected error from embedder")
	}
	if !strings.Contains(err.Error(), "embed query") {
		t.Errorf("error should mention embedding: %v", err)
	}
}

func TestService_CreateStore(t *testing.T) {
	svc, _, mockStore, _ := newTestService(t)
	ctx := context.Background()

	err := svc.CreateStore(ctx, "tenant1", "store1")

	if err != nil {
		t.Fatalf("CreateStore failed: %v", err)
	}

	if len(mockStore.CreateCollectionCalls) != 1 {
		t.Fatal("expected CreateCollection to be called")
	}

	if mockStore.CreateCollectionCalls[0].Name != "tenant1_store1" {
		t.Errorf("wrong collection name: %s", mockStore.CreateCollectionCalls[0].Name)
	}
}

func TestService_DeleteStore(t *testing.T) {
	svc, _, mockStore, _ := newTestService(t)
	ctx := context.Background()

	mockStore.CreateCollection(ctx, "tenant1_store1", 768)

	err := svc.DeleteStore(ctx, "tenant1", "store1")

	if err != nil {
		t.Fatalf("DeleteStore failed: %v", err)
	}

	exists, _ := mockStore.CollectionExists(ctx, "tenant1_store1")
	if exists {
		t.Error("collection should be deleted")
	}
}

func TestService_StoreInfo(t *testing.T) {
	svc, _, mockStore, _ := newTestService(t)
	ctx := context.Background()

	mockStore.CreateCollection(ctx, "tenant1_store1", 768)
	mockStore.Upsert(ctx, "tenant1_store1", []vectorstore.Point{
		{ID: "1", Vector: testutil.RandomEmbedding(768)},
		{ID: "2", Vector: testutil.RandomEmbedding(768)},
	})

	info, err := svc.StoreInfo(ctx, "tenant1", "store1")

	if err != nil {
		t.Fatalf("StoreInfo failed: %v", err)
	}

	if info.Name != "tenant1_store1" {
		t.Errorf("expected name=tenant1_store1, got %s", info.Name)
	}
	if info.PointCount != 2 {
		t.Errorf("expected 2 points, got %d", info.PointCount)
	}
}

func TestService_CollectionName(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	name := svc.collectionName("tenant1", "store1")
	if name != "tenant1_store1" {
		t.Errorf("expected tenant1_store1, got %s", name)
	}
}

func TestGetString(t *testing.T) {
	tests := []struct {
		m    map[string]any
		key  string
		want string
	}{
		{map[string]any{"foo": "bar"}, "foo", "bar"},
		{map[string]any{"foo": 123}, "foo", ""},
		{map[string]any{}, "foo", ""},
		{nil, "foo", ""},
	}

	for _, tt := range tests {
		got := getString(tt.m, tt.key)
		if got != tt.want {
			t.Errorf("getString(%v, %q) = %q, want %q", tt.m, tt.key, got, tt.want)
		}
	}
}

func TestGetInt(t *testing.T) {
	tests := []struct {
		m    map[string]any
		key  string
		want int
	}{
		{map[string]any{"foo": 42}, "foo", 42},
		{map[string]any{"foo": int64(42)}, "foo", 42},
		{map[string]any{"foo": float64(42.0)}, "foo", 42},
		{map[string]any{"foo": "not int"}, "foo", 0},
		{map[string]any{}, "foo", 0},
		{nil, "foo", 0},
	}

	for _, tt := range tests {
		got := getInt(tt.m, tt.key)
		if got != tt.want {
			t.Errorf("getInt(%v, %q) = %d, want %d", tt.m, tt.key, got, tt.want)
		}
	}
}

func TestDefaultServiceOptions(t *testing.T) {
	opts := DefaultServiceOptions()

	if opts.ChunkSize != 2000 {
		t.Errorf("expected ChunkSize=2000, got %d", opts.ChunkSize)
	}
	if opts.ChunkOverlap != 200 {
		t.Errorf("expected ChunkOverlap=200, got %d", opts.ChunkOverlap)
	}
	if opts.RetrievalTopK != 5 {
		t.Errorf("expected RetrievalTopK=5, got %d", opts.RetrievalTopK)
	}
}
