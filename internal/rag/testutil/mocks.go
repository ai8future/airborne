// Package testutil provides test utilities and mocks for the RAG package.
package testutil

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"sync"

	"github.com/cliffpyles/aibox/internal/rag/extractor"
	"github.com/cliffpyles/aibox/internal/rag/vectorstore"
)

// MockEmbedder is a configurable mock for the Embedder interface.
type MockEmbedder struct {
	mu sync.Mutex

	// Function hooks for custom behavior
	EmbedFunc      func(ctx context.Context, text string) ([]float32, error)
	EmbedBatchFunc func(ctx context.Context, texts []string) ([][]float32, error)

	// Configuration
	Dims      int
	ModelName string

	// Call tracking
	EmbedCalls      []string
	EmbedBatchCalls [][]string
}

// NewMockEmbedder creates a new mock embedder with default behavior.
func NewMockEmbedder(dims int) *MockEmbedder {
	return &MockEmbedder{
		Dims:      dims,
		ModelName: "mock-embed",
	}
}

// Embed generates a mock embedding.
func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	m.EmbedCalls = append(m.EmbedCalls, text)
	m.mu.Unlock()

	if m.EmbedFunc != nil {
		return m.EmbedFunc(ctx, text)
	}

	return RandomEmbedding(m.Dims), nil
}

// EmbedBatch generates mock embeddings for multiple texts.
func (m *MockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	m.mu.Lock()
	m.EmbedBatchCalls = append(m.EmbedBatchCalls, texts)
	m.mu.Unlock()

	if m.EmbedBatchFunc != nil {
		return m.EmbedBatchFunc(ctx, texts)
	}

	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = RandomEmbedding(m.Dims)
	}
	return embeddings, nil
}

// Dimensions returns the configured dimensions.
func (m *MockEmbedder) Dimensions() int {
	return m.Dims
}

// Model returns the mock model name.
func (m *MockEmbedder) Model() string {
	return m.ModelName
}

// Reset clears call tracking.
func (m *MockEmbedder) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EmbedCalls = nil
	m.EmbedBatchCalls = nil
}

// MockStore is a configurable mock for the VectorStore interface.
type MockStore struct {
	mu sync.Mutex

	// In-memory storage
	collections map[string]*mockCollection

	// Function hooks for custom behavior
	CreateCollectionFunc func(ctx context.Context, name string, dimensions int) error
	DeleteCollectionFunc func(ctx context.Context, name string) error
	CollectionExistsFunc func(ctx context.Context, name string) (bool, error)
	CollectionInfoFunc   func(ctx context.Context, name string) (*vectorstore.CollectionInfo, error)
	UpsertFunc           func(ctx context.Context, collection string, points []vectorstore.Point) error
	SearchFunc           func(ctx context.Context, params vectorstore.SearchParams) ([]vectorstore.SearchResult, error)
	DeleteFunc           func(ctx context.Context, collection string, ids []string) error

	// Call tracking
	CreateCollectionCalls []createCollectionCall
	UpsertCalls           []upsertCall
	SearchCalls           []vectorstore.SearchParams
}

type createCollectionCall struct {
	Name       string
	Dimensions int
}

type upsertCall struct {
	Collection string
	Points     []vectorstore.Point
}

type mockCollection struct {
	name       string
	dimensions int
	points     map[string]vectorstore.Point
}

// NewMockStore creates a new mock store.
func NewMockStore() *MockStore {
	return &MockStore{
		collections: make(map[string]*mockCollection),
	}
}

// CreateCollection creates an in-memory collection.
func (m *MockStore) CreateCollection(ctx context.Context, name string, dimensions int) error {
	m.mu.Lock()
	m.CreateCollectionCalls = append(m.CreateCollectionCalls, createCollectionCall{name, dimensions})
	m.mu.Unlock()

	if m.CreateCollectionFunc != nil {
		return m.CreateCollectionFunc(ctx, name, dimensions)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.collections[name] = &mockCollection{
		name:       name,
		dimensions: dimensions,
		points:     make(map[string]vectorstore.Point),
	}
	return nil
}

// DeleteCollection removes a collection.
func (m *MockStore) DeleteCollection(ctx context.Context, name string) error {
	if m.DeleteCollectionFunc != nil {
		return m.DeleteCollectionFunc(ctx, name)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.collections, name)
	return nil
}

// CollectionExists checks if a collection exists.
func (m *MockStore) CollectionExists(ctx context.Context, name string) (bool, error) {
	if m.CollectionExistsFunc != nil {
		return m.CollectionExistsFunc(ctx, name)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.collections[name]
	return exists, nil
}

// CollectionInfo returns collection metadata.
func (m *MockStore) CollectionInfo(ctx context.Context, name string) (*vectorstore.CollectionInfo, error) {
	if m.CollectionInfoFunc != nil {
		return m.CollectionInfoFunc(ctx, name)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	coll, exists := m.collections[name]
	if !exists {
		return nil, fmt.Errorf("collection not found: %s", name)
	}

	return &vectorstore.CollectionInfo{
		Name:       name,
		PointCount: int64(len(coll.points)),
		Dimensions: coll.dimensions,
	}, nil
}

// Upsert adds points to a collection.
func (m *MockStore) Upsert(ctx context.Context, collection string, points []vectorstore.Point) error {
	m.mu.Lock()
	m.UpsertCalls = append(m.UpsertCalls, upsertCall{collection, points})
	m.mu.Unlock()

	if m.UpsertFunc != nil {
		return m.UpsertFunc(ctx, collection, points)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	coll, exists := m.collections[collection]
	if !exists {
		return fmt.Errorf("collection not found: %s", collection)
	}

	for _, p := range points {
		coll.points[p.ID] = p
	}
	return nil
}

// Search finds similar points (returns mock results).
func (m *MockStore) Search(ctx context.Context, params vectorstore.SearchParams) ([]vectorstore.SearchResult, error) {
	m.mu.Lock()
	m.SearchCalls = append(m.SearchCalls, params)
	m.mu.Unlock()

	if m.SearchFunc != nil {
		return m.SearchFunc(ctx, params)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	coll, exists := m.collections[params.Collection]
	if !exists {
		return nil, nil
	}

	// Return up to Limit points
	var results []vectorstore.SearchResult
	for id, p := range coll.points {
		if len(results) >= params.Limit {
			break
		}
		results = append(results, vectorstore.SearchResult{
			ID:      id,
			Score:   0.9,
			Payload: p.Payload,
		})
	}
	return results, nil
}

// Delete removes points by ID.
func (m *MockStore) Delete(ctx context.Context, collection string, ids []string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, collection, ids)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	coll, exists := m.collections[collection]
	if !exists {
		return nil
	}

	for _, id := range ids {
		delete(coll.points, id)
	}
	return nil
}

// Reset clears all data and call tracking.
func (m *MockStore) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collections = make(map[string]*mockCollection)
	m.CreateCollectionCalls = nil
	m.UpsertCalls = nil
	m.SearchCalls = nil
}

// GetPoints returns all points in a collection (for testing).
func (m *MockStore) GetPoints(collection string) []vectorstore.Point {
	m.mu.Lock()
	defer m.mu.Unlock()
	coll, exists := m.collections[collection]
	if !exists {
		return nil
	}

	points := make([]vectorstore.Point, 0, len(coll.points))
	for _, p := range coll.points {
		points = append(points, p)
	}
	return points
}

// MockExtractor is a configurable mock for the Extractor interface.
type MockExtractor struct {
	mu sync.Mutex

	// Function hook for custom behavior
	ExtractFunc func(ctx context.Context, file io.Reader, filename, mimeType string) (*extractor.ExtractionResult, error)

	// Default response
	DefaultText string

	// Call tracking
	ExtractCalls []extractCall
}

type extractCall struct {
	Filename string
	MIMEType string
	Content  []byte
}

// NewMockExtractor creates a new mock extractor.
func NewMockExtractor() *MockExtractor {
	return &MockExtractor{
		DefaultText: "This is extracted text from the document.",
	}
}

// Extract returns mock extraction results.
func (m *MockExtractor) Extract(ctx context.Context, file io.Reader, filename, mimeType string) (*extractor.ExtractionResult, error) {
	content, _ := io.ReadAll(file)

	m.mu.Lock()
	m.ExtractCalls = append(m.ExtractCalls, extractCall{filename, mimeType, content})
	m.mu.Unlock()

	if m.ExtractFunc != nil {
		return m.ExtractFunc(ctx, nil, filename, mimeType)
	}

	return &extractor.ExtractionResult{
		Text:      m.DefaultText,
		PageCount: 1,
		Metadata:  map[string]any{"mock": true},
	}, nil
}

// SupportedFormats returns mock supported formats.
func (m *MockExtractor) SupportedFormats() []string {
	return []string{".pdf", ".docx", ".txt", ".md"}
}

// Reset clears call tracking.
func (m *MockExtractor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExtractCalls = nil
}

// Helper functions

// RandomEmbedding generates a random embedding vector.
func RandomEmbedding(dims int) []float32 {
	embedding := make([]float32, dims)
	for i := range embedding {
		embedding[i] = rand.Float32()
	}
	return embedding
}

// SampleText generates sample text of approximately the given size.
func SampleText(size int) string {
	words := []string{"the", "quick", "brown", "fox", "jumps", "over", "lazy", "dog"}
	var result string
	for len(result) < size {
		for _, w := range words {
			result += w + " "
			if len(result) >= size {
				break
			}
		}
	}
	return result[:size]
}
