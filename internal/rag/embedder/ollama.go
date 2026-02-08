package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ai8future/chassis-go/v5/call"
)

// OllamaEmbedder generates embeddings using Ollama's API.
type OllamaEmbedder struct {
	baseURL    string
	model      string
	dimensions int
	client     *call.Client
}

// OllamaConfig configures the Ollama embedder.
type OllamaConfig struct {
	// BaseURL is the Ollama API base URL (default: http://localhost:11434).
	BaseURL string

	// Model is the embedding model to use (default: nomic-embed-text).
	Model string

	// Timeout is the HTTP request timeout (default: 30s).
	Timeout time.Duration
}

// modelDimensions maps known models to their embedding dimensions.
var modelDimensions = map[string]int{
	"nomic-embed-text":    768,
	"mxbai-embed-large":   1024,
	"bge-m3":              1024,
	"bge-large-en-v1.5":   1024,
	"all-minilm":          384,
	"snowflake-arctic-embed": 1024,
}

// NewOllamaEmbedder creates a new Ollama embedder.
func NewOllamaEmbedder(cfg OllamaConfig) *OllamaEmbedder {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "nomic-embed-text"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	dimensions := 768 // default
	if d, ok := modelDimensions[cfg.Model]; ok {
		dimensions = d
	}

	return &OllamaEmbedder{
		baseURL:    cfg.BaseURL,
		model:      cfg.Model,
		dimensions: dimensions,
		client: call.New(
			call.WithTimeout(cfg.Timeout),
			call.WithRetry(3, 500*time.Millisecond),
			call.WithCircuitBreaker("ollama", 5, 30*time.Second),
		),
	}
}

// ollamaEmbedRequest is the request body for Ollama's embedding API.
type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbedResponse is the response from Ollama's embedding API.
type ollamaEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

// Embed generates an embedding for a single text.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbedRequest{
		Model:  e.model,
		Prompt: text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var embedResp ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Convert float64 to float32
	embedding := make([]float32, len(embedResp.Embedding))
	for i, v := range embedResp.Embedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// EmbedBatch generates embeddings for multiple texts.
// Ollama doesn't have a native batch API, so we call Embed for each text.
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))

	for i, text := range texts {
		embedding, err := e.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embed text %d: %w", i, err)
		}
		embeddings[i] = embedding
	}

	return embeddings, nil
}

// Dimensions returns the embedding dimensionality.
func (e *OllamaEmbedder) Dimensions() int {
	return e.dimensions
}

// Model returns the model name.
func (e *OllamaEmbedder) Model() string {
	return e.model
}

// Ping checks connectivity to the Ollama server.
func (e *OllamaEmbedder) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check returned %d", resp.StatusCode)
	}
	return nil
}
