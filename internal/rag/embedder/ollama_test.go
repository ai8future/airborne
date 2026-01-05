package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewOllamaEmbedder_Defaults(t *testing.T) {
	emb := NewOllamaEmbedder(OllamaConfig{})

	if emb.baseURL != "http://localhost:11434" {
		t.Errorf("expected default baseURL, got %s", emb.baseURL)
	}
	if emb.model != "nomic-embed-text" {
		t.Errorf("expected default model, got %s", emb.model)
	}
	if emb.dimensions != 768 {
		t.Errorf("expected default dimensions=768, got %d", emb.dimensions)
	}
}

func TestNewOllamaEmbedder_CustomConfig(t *testing.T) {
	emb := NewOllamaEmbedder(OllamaConfig{
		BaseURL: "http://custom:1234",
		Model:   "bge-m3",
		Timeout: 60 * time.Second,
	})

	if emb.baseURL != "http://custom:1234" {
		t.Errorf("expected custom baseURL, got %s", emb.baseURL)
	}
	if emb.model != "bge-m3" {
		t.Errorf("expected custom model, got %s", emb.model)
	}
	if emb.dimensions != 1024 {
		t.Errorf("expected bge-m3 dimensions=1024, got %d", emb.dimensions)
	}
}

func TestOllamaEmbedder_Dimensions(t *testing.T) {
	tests := []struct {
		model string
		dims  int
	}{
		{"nomic-embed-text", 768},
		{"mxbai-embed-large", 1024},
		{"bge-m3", 1024},
		{"bge-large-en-v1.5", 1024},
		{"all-minilm", 384},
		{"unknown-model", 768}, // default
	}

	for _, tt := range tests {
		emb := NewOllamaEmbedder(OllamaConfig{Model: tt.model})
		if emb.Dimensions() != tt.dims {
			t.Errorf("model %s: expected dims=%d, got %d", tt.model, tt.dims, emb.Dimensions())
		}
	}
}

func TestOllamaEmbedder_Model(t *testing.T) {
	emb := NewOllamaEmbedder(OllamaConfig{Model: "test-model"})
	if emb.Model() != "test-model" {
		t.Errorf("expected model=test-model, got %s", emb.Model())
	}
}

func TestOllamaEmbedder_Embed_Success(t *testing.T) {
	expectedDims := 768
	embedding := make([]float64, expectedDims)
	for i := range embedding {
		embedding[i] = float64(i) / float64(expectedDims)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("expected /api/embeddings, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content-type")
		}

		// Verify request body
		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.Model != "nomic-embed-text" {
			t.Errorf("expected model=nomic-embed-text, got %s", req.Model)
		}
		if req.Prompt != "test input" {
			t.Errorf("expected prompt='test input', got %s", req.Prompt)
		}

		// Send response
		json.NewEncoder(w).Encode(ollamaEmbedResponse{Embedding: embedding})
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{BaseURL: server.URL})
	result, err := emb.Embed(context.Background(), "test input")

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(result) != expectedDims {
		t.Errorf("expected %d dimensions, got %d", expectedDims, len(result))
	}

	// Verify values converted correctly
	if result[0] != float32(0) {
		t.Errorf("expected first value=0, got %f", result[0])
	}
}

func TestOllamaEmbedder_Embed_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{BaseURL: server.URL})
	_, err := emb.Embed(context.Background(), "test")

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code: %v", err)
	}
}

func TestOllamaEmbedder_Embed_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{BaseURL: server.URL})
	_, err := emb.Embed(context.Background(), "test")

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error should mention decoding: %v", err)
	}
}

func TestOllamaEmbedder_Embed_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		json.NewEncoder(w).Encode(ollamaEmbedResponse{Embedding: []float64{1.0}})
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{
		BaseURL: server.URL,
		Timeout: 50 * time.Millisecond,
	})
	_, err := emb.Embed(context.Background(), "test")

	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestOllamaEmbedder_Embed_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		json.NewEncoder(w).Encode(ollamaEmbedResponse{Embedding: []float64{1.0}})
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{BaseURL: server.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := emb.Embed(ctx, "test")

	if err == nil {
		t.Fatal("expected context canceled error")
	}
}

func TestOllamaEmbedder_EmbedBatch_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		embedding := make([]float64, 768)
		for i := range embedding {
			embedding[i] = float64(callCount)
		}
		json.NewEncoder(w).Encode(ollamaEmbedResponse{Embedding: embedding})
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{BaseURL: server.URL})
	texts := []string{"first", "second", "third"}
	results, err := emb.EmbedBatch(context.Background(), texts)

	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Verify each was called
	if callCount != 3 {
		t.Errorf("expected 3 API calls, got %d", callCount)
	}

	// Verify different embeddings
	if results[0][0] == results[1][0] {
		t.Error("expected different embeddings for different inputs")
	}
}

func TestOllamaEmbedder_EmbedBatch_PartialFailure(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(ollamaEmbedResponse{Embedding: make([]float64, 768)})
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{BaseURL: server.URL})
	texts := []string{"first", "second", "third"}
	_, err := emb.EmbedBatch(context.Background(), texts)

	if err == nil {
		t.Fatal("expected error on partial failure")
	}
	if !strings.Contains(err.Error(), "text 1") {
		t.Errorf("error should indicate which text failed: %v", err)
	}
}

func TestOllamaEmbedder_EmbedBatch_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call server for empty batch")
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{BaseURL: server.URL})
	results, err := emb.EmbedBatch(context.Background(), []string{})

	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestOllamaEmbedder_ConnectionError(t *testing.T) {
	// Use a port that's not listening
	emb := NewOllamaEmbedder(OllamaConfig{
		BaseURL: "http://localhost:1",
		Timeout: 100 * time.Millisecond,
	})

	_, err := emb.Embed(context.Background(), "test")

	if err == nil {
		t.Fatal("expected connection error")
	}
}

// Benchmark
func BenchmarkOllamaEmbedder_Embed(b *testing.B) {
	embedding := make([]float64, 768)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ollamaEmbedResponse{Embedding: embedding})
	}))
	defer server.Close()

	emb := NewOllamaEmbedder(OllamaConfig{BaseURL: server.URL})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		emb.Embed(ctx, "benchmark text")
	}
}
