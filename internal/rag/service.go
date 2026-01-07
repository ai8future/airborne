// Package rag provides Retrieval-Augmented Generation capabilities.
package rag

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/cliffpyles/aibox/internal/rag/chunker"
	"github.com/cliffpyles/aibox/internal/rag/embedder"
	"github.com/cliffpyles/aibox/internal/rag/extractor"
	"github.com/cliffpyles/aibox/internal/rag/vectorstore"
)

const maxCollectionPartLen = 128

var collectionPartPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

func validateCollectionParts(tenantID, storeID string) error {
	tenantID = strings.TrimSpace(tenantID)
	storeID = strings.TrimSpace(storeID)

	if tenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if storeID == "" {
		return fmt.Errorf("store_id is required")
	}
	if len(tenantID) > maxCollectionPartLen {
		return fmt.Errorf("tenant_id exceeds %d characters", maxCollectionPartLen)
	}
	if len(storeID) > maxCollectionPartLen {
		return fmt.Errorf("store_id exceeds %d characters", maxCollectionPartLen)
	}
	if !collectionPartPattern.MatchString(tenantID) {
		return fmt.Errorf("tenant_id contains invalid characters")
	}
	if !collectionPartPattern.MatchString(storeID) {
		return fmt.Errorf("store_id contains invalid characters")
	}
	return nil
}

// Service orchestrates RAG operations: ingest files and retrieve relevant chunks.
type Service struct {
	embedder  embedder.Embedder
	store     vectorstore.Store
	extractor extractor.Extractor
	opts      ServiceOptions
}

// ServiceOptions configures the RAG service.
type ServiceOptions struct {
	// ChunkSize is the target chunk size in characters.
	ChunkSize int

	// ChunkOverlap is the overlap between chunks in characters.
	ChunkOverlap int

	// RetrievalTopK is the default number of chunks to retrieve.
	RetrievalTopK int
}

// DefaultServiceOptions returns sensible defaults.
func DefaultServiceOptions() ServiceOptions {
	return ServiceOptions{
		ChunkSize:     2000,
		ChunkOverlap:  200,
		RetrievalTopK: 5,
	}
}

// NewService creates a new RAG service.
func NewService(
	emb embedder.Embedder,
	store vectorstore.Store,
	ext extractor.Extractor,
	opts ServiceOptions,
) *Service {
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 2000
	}
	if opts.ChunkOverlap <= 0 {
		opts.ChunkOverlap = 200
	}
	if opts.RetrievalTopK <= 0 {
		opts.RetrievalTopK = 5
	}

	return &Service{
		embedder:  emb,
		store:     store,
		extractor: ext,
		opts:      opts,
	}
}

// IngestParams contains parameters for file ingestion.
type IngestParams struct {
	// StoreID is the file store identifier (used as collection name prefix).
	StoreID string

	// TenantID is the tenant identifier for multi-tenancy.
	TenantID string

	// ThreadID is the conversation thread ID (optional, for scoping).
	ThreadID string

	// File is the file content to ingest.
	File io.Reader

	// Filename is the original filename.
	Filename string

	// MIMEType is the file's MIME type.
	MIMEType string
}

// IngestResult contains the result of file ingestion.
type IngestResult struct {
	// ChunkCount is the number of chunks created.
	ChunkCount int

	// CollectionName is the Qdrant collection name.
	CollectionName string
}

// Ingest extracts text from a file, chunks it, embeds the chunks, and stores them.
func (s *Service) Ingest(ctx context.Context, params IngestParams) (*IngestResult, error) {
	if err := validateCollectionParts(params.TenantID, params.StoreID); err != nil {
		return nil, err
	}

	// Generate collection name
	collectionName := s.collectionName(params.TenantID, params.StoreID)

	// Ensure collection exists
	exists, err := s.store.CollectionExists(ctx, collectionName)
	if err != nil {
		return nil, fmt.Errorf("check collection: %w", err)
	}
	if !exists {
		if err := s.store.CreateCollection(ctx, collectionName, s.embedder.Dimensions()); err != nil {
			return nil, fmt.Errorf("create collection: %w", err)
		}
	}

	// Extract text from file
	result, err := s.extractor.Extract(ctx, params.File, params.Filename, params.MIMEType)
	if err != nil {
		return nil, fmt.Errorf("extract text: %w", err)
	}

	if len(result.Text) == 0 {
		return &IngestResult{
			ChunkCount:     0,
			CollectionName: collectionName,
		}, nil
	}

	// Chunk the text
	chunks := chunker.ChunkText(result.Text, chunker.Options{
		ChunkSize:    s.opts.ChunkSize,
		Overlap:      s.opts.ChunkOverlap,
		MinChunkSize: 100,
	})

	if len(chunks) == 0 {
		return &IngestResult{
			ChunkCount:     0,
			CollectionName: collectionName,
		}, nil
	}

	// Extract chunk texts for batch embedding
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Text
	}

	// Generate embeddings
	embeddings, err := s.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("generate embeddings: %w", err)
	}

	// Create points for vector store
	points := make([]vectorstore.Point, len(chunks))
	for i, chunk := range chunks {
		points[i] = vectorstore.Point{
			ID:     fmt.Sprintf("%s_%s_%d", params.Filename, params.StoreID, chunk.Index),
			Vector: embeddings[i],
			Payload: map[string]any{
				"tenant_id":   params.TenantID,
				"thread_id":   params.ThreadID,
				"store_id":    params.StoreID,
				"filename":    params.Filename,
				"chunk_index": chunk.Index,
				"text":        chunk.Text,
				"char_start":  chunk.Start,
				"char_end":    chunk.End,
			},
		}
	}

	// Store in vector database
	if err := s.store.Upsert(ctx, collectionName, points); err != nil {
		return nil, fmt.Errorf("store embeddings: %w", err)
	}

	return &IngestResult{
		ChunkCount:     len(chunks),
		CollectionName: collectionName,
	}, nil
}

// RetrieveParams contains parameters for chunk retrieval.
type RetrieveParams struct {
	// StoreID is the file store identifier.
	StoreID string

	// TenantID is the tenant identifier.
	TenantID string

	// Query is the text to find similar chunks for.
	Query string

	// TopK is the number of chunks to retrieve (default: service's RetrievalTopK).
	TopK int

	// ThreadID optionally filters to a specific thread.
	ThreadID string
}

// RetrieveResult is a single retrieved chunk.
type RetrieveResult struct {
	// Text is the chunk content.
	Text string

	// Filename is the source filename.
	Filename string

	// ChunkIndex is the chunk's position in the source file.
	ChunkIndex int

	// Score is the similarity score.
	Score float32
}

// Retrieve finds chunks similar to the query text.
func (s *Service) Retrieve(ctx context.Context, params RetrieveParams) ([]RetrieveResult, error) {
	if err := validateCollectionParts(params.TenantID, params.StoreID); err != nil {
		return nil, err
	}

	collectionName := s.collectionName(params.TenantID, params.StoreID)

	// Check if collection exists
	exists, err := s.store.CollectionExists(ctx, collectionName)
	if err != nil {
		return nil, fmt.Errorf("check collection: %w", err)
	}
	if !exists {
		// No documents have been ingested yet
		return nil, nil
	}

	// Embed the query
	queryVector, err := s.embedder.Embed(ctx, params.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	topK := params.TopK
	if topK <= 0 {
		topK = s.opts.RetrievalTopK
	}

	// Build filter
	var filter *vectorstore.Filter
	if params.ThreadID != "" {
		filter = &vectorstore.Filter{
			Must: []vectorstore.Condition{
				{Field: "thread_id", Match: params.ThreadID},
			},
		}
	}

	// Search
	results, err := s.store.Search(ctx, vectorstore.SearchParams{
		Collection: collectionName,
		Vector:     queryVector,
		Limit:      topK,
		Filter:     filter,
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Convert to RetrieveResult
	retrieved := make([]RetrieveResult, len(results))
	for i, r := range results {
		retrieved[i] = RetrieveResult{
			Text:       getString(r.Payload, "text"),
			Filename:   getString(r.Payload, "filename"),
			ChunkIndex: getInt(r.Payload, "chunk_index"),
			Score:      r.Score,
		}
	}

	return retrieved, nil
}

// CreateStore creates a new file store (Qdrant collection).
func (s *Service) CreateStore(ctx context.Context, tenantID, storeID string) error {
	if err := validateCollectionParts(tenantID, storeID); err != nil {
		return err
	}
	collectionName := s.collectionName(tenantID, storeID)
	return s.store.CreateCollection(ctx, collectionName, s.embedder.Dimensions())
}

// DeleteStore removes a file store and all its contents.
func (s *Service) DeleteStore(ctx context.Context, tenantID, storeID string) error {
	if err := validateCollectionParts(tenantID, storeID); err != nil {
		return err
	}
	collectionName := s.collectionName(tenantID, storeID)
	return s.store.DeleteCollection(ctx, collectionName)
}

// StoreInfo returns information about a file store.
func (s *Service) StoreInfo(ctx context.Context, tenantID, storeID string) (*vectorstore.CollectionInfo, error) {
	if err := validateCollectionParts(tenantID, storeID); err != nil {
		return nil, err
	}
	collectionName := s.collectionName(tenantID, storeID)
	return s.store.CollectionInfo(ctx, collectionName)
}

// collectionName generates a Qdrant collection name from tenant and store IDs.
func (s *Service) collectionName(tenantID, storeID string) string {
	return fmt.Sprintf("%s_%s", tenantID, storeID)
}

// Helper functions for payload extraction
func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}
