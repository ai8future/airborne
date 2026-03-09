package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ai8future/chassis-go/v9/call"
)

// QdrantStore implements the Store interface using Qdrant's REST API.
type QdrantStore struct {
	baseURL string
	client  *call.Client
}

// QdrantConfig configures the Qdrant store.
type QdrantConfig struct {
	// BaseURL is the Qdrant REST API base URL (default: http://localhost:6333).
	BaseURL string

	// Timeout is the HTTP request timeout (default: 30s).
	Timeout time.Duration
}

// NewQdrantStore creates a new Qdrant store.
func NewQdrantStore(cfg QdrantConfig) *QdrantStore {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:6333"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &QdrantStore{
		baseURL: cfg.BaseURL,
		client: call.New(
			call.WithTimeout(cfg.Timeout),
			call.WithRetry(3, 500*time.Millisecond),
			call.WithCircuitBreaker("qdrant", 5, 30*time.Second),
		),
	}
}

// CreateCollection creates a new collection with the specified dimensions.
func (s *QdrantStore) CreateCollection(ctx context.Context, name string, dimensions int) error {
	body := map[string]any{
		"vectors": map[string]any{
			"size":     dimensions,
			"distance": "Cosine",
		},
	}

	_, err := s.doRequest(ctx, http.MethodPut, "/collections/"+name, body)
	return err
}

// DeleteCollection removes a collection.
func (s *QdrantStore) DeleteCollection(ctx context.Context, name string) error {
	_, err := s.doRequest(ctx, http.MethodDelete, "/collections/"+name, nil)
	return err
}

// CollectionExists checks if a collection exists.
func (s *QdrantStore) CollectionExists(ctx context.Context, name string) (bool, error) {
	resp, err := s.doRequestRaw(ctx, http.MethodGet, "/collections/"+name, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("qdrant error (status %d): %s", resp.StatusCode, string(body))
	}

	return true, nil
}

// CollectionInfo returns metadata about a collection.
func (s *QdrantStore) CollectionInfo(ctx context.Context, name string) (*CollectionInfo, error) {
	resp, err := s.doRequest(ctx, http.MethodGet, "/collections/"+name, nil)
	if err != nil {
		return nil, err
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response format")
	}

	var pointCount int64
	if pc, ok := result["points_count"].(float64); ok {
		pointCount = int64(pc)
	}

	var dimensions int
	if config, ok := result["config"].(map[string]any); ok {
		if params, ok := config["params"].(map[string]any); ok {
			if vectors, ok := params["vectors"].(map[string]any); ok {
				if size, ok := vectors["size"].(float64); ok {
					dimensions = int(size)
				}
			}
		}
	}

	return &CollectionInfo{
		Name:       name,
		PointCount: pointCount,
		Dimensions: dimensions,
	}, nil
}

// Upsert adds or updates points in a collection.
func (s *QdrantStore) Upsert(ctx context.Context, collection string, points []Point) error {
	qdrantPoints := make([]map[string]any, len(points))
	for i, p := range points {
		qdrantPoints[i] = map[string]any{
			"id":      p.ID,
			"vector":  p.Vector,
			"payload": p.Payload,
		}
	}

	body := map[string]any{
		"points": qdrantPoints,
	}

	_, err := s.doRequest(ctx, http.MethodPut, "/collections/"+collection+"/points?wait=true", body)
	return err
}

// Search finds similar points.
func (s *QdrantStore) Search(ctx context.Context, params SearchParams) ([]SearchResult, error) {
	body := map[string]any{
		"vector":       params.Vector,
		"limit":        params.Limit,
		"with_payload": true,
	}

	if params.Filter != nil && len(params.Filter.Must) > 0 {
		mustConditions := make([]map[string]any, len(params.Filter.Must))
		for i, cond := range params.Filter.Must {
			mustConditions[i] = map[string]any{
				"key":   cond.Field,
				"match": map[string]any{"value": cond.Match},
			}
		}
		body["filter"] = map[string]any{
			"must": mustConditions,
		}
	}

	if params.ScoreThreshold > 0 {
		body["score_threshold"] = params.ScoreThreshold
	}

	resp, err := s.doRequest(ctx, http.MethodPost, "/collections/"+params.Collection+"/points/search", body)
	if err != nil {
		return nil, err
	}

	resultsRaw, ok := resp["result"].([]any)
	if !ok {
		return nil, nil
	}

	results := make([]SearchResult, 0, len(resultsRaw))
	for _, r := range resultsRaw {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}

		result := SearchResult{}

		// Handle ID (can be string or number)
		switch id := rm["id"].(type) {
		case string:
			result.ID = id
		case float64:
			result.ID = fmt.Sprintf("%d", int64(id))
		}

		if score, ok := rm["score"].(float64); ok {
			result.Score = float32(score)
		}

		if payload, ok := rm["payload"].(map[string]any); ok {
			result.Payload = payload
		}

		results = append(results, result)
	}

	return results, nil
}

// Delete removes points by ID.
func (s *QdrantStore) Delete(ctx context.Context, collection string, ids []string) error {
	body := map[string]any{
		"points": ids,
	}

	_, err := s.doRequest(ctx, http.MethodPost, "/collections/"+collection+"/points/delete?wait=true", body)
	return err
}

// doRequest sends an HTTP request and decodes the JSON response.
func (s *QdrantStore) doRequest(ctx context.Context, method, path string, body any) (map[string]any, error) {
	resp, err := s.doRequestRaw(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("qdrant error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result, nil
}

// doRequestRaw sends an HTTP request and returns the raw response.
func (s *QdrantStore) doRequestRaw(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return s.client.Do(req)
}

// Ping checks connectivity to the Qdrant server.
func (s *QdrantStore) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant health check returned %d", resp.StatusCode)
	}
	return nil
}
