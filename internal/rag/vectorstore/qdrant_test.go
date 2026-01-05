package vectorstore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewQdrantStore_Defaults(t *testing.T) {
	store := NewQdrantStore(QdrantConfig{})

	if store.baseURL != "http://localhost:6333" {
		t.Errorf("expected default baseURL, got %s", store.baseURL)
	}
}

func TestNewQdrantStore_CustomConfig(t *testing.T) {
	store := NewQdrantStore(QdrantConfig{
		BaseURL: "http://custom:1234",
		Timeout: 60 * time.Second,
	})

	if store.baseURL != "http://custom:1234" {
		t.Errorf("expected custom baseURL, got %s", store.baseURL)
	}
}

func TestQdrantStore_CreateCollection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/collections/") {
			t.Errorf("expected /collections/ path, got %s", r.URL.Path)
		}

		// Verify request body
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		vectors, ok := body["vectors"].(map[string]any)
		if !ok {
			t.Error("expected vectors in request body")
		}
		if vectors["size"] != float64(768) {
			t.Errorf("expected size=768, got %v", vectors["size"])
		}
		if vectors["distance"] != "Cosine" {
			t.Errorf("expected distance=Cosine, got %v", vectors["distance"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	err := store.CreateCollection(context.Background(), "test_collection", 768)

	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
}

func TestQdrantStore_CreateCollection_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	err := store.CreateCollection(context.Background(), "test_collection", 768)

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code: %v", err)
	}
}

func TestQdrantStore_DeleteCollection_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	err := store.DeleteCollection(context.Background(), "test_collection")

	if err != nil {
		t.Fatalf("DeleteCollection failed: %v", err)
	}
}

func TestQdrantStore_CollectionExists_True(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"points_count": 100,
			},
		})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	exists, err := store.CollectionExists(context.Background(), "test_collection")

	if err != nil {
		t.Fatalf("CollectionExists failed: %v", err)
	}
	if !exists {
		t.Error("expected collection to exist")
	}
}

func TestQdrantStore_CollectionExists_False(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	exists, err := store.CollectionExists(context.Background(), "nonexistent")

	if err != nil {
		t.Fatalf("CollectionExists failed: %v", err)
	}
	if exists {
		t.Error("expected collection to not exist")
	}
}

func TestQdrantStore_CollectionInfo_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"points_count": float64(42),
				"config": map[string]any{
					"params": map[string]any{
						"vectors": map[string]any{
							"size": float64(768),
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	info, err := store.CollectionInfo(context.Background(), "test_collection")

	if err != nil {
		t.Fatalf("CollectionInfo failed: %v", err)
	}
	if info.Name != "test_collection" {
		t.Errorf("expected name=test_collection, got %s", info.Name)
	}
	if info.PointCount != 42 {
		t.Errorf("expected PointCount=42, got %d", info.PointCount)
	}
	if info.Dimensions != 768 {
		t.Errorf("expected Dimensions=768, got %d", info.Dimensions)
	}
}

func TestQdrantStore_Upsert_Success(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/points") {
			t.Errorf("expected /points in path, got %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "wait=true") {
			t.Error("expected wait=true in query")
		}

		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	points := []Point{
		{
			ID:      "point1",
			Vector:  []float32{0.1, 0.2, 0.3},
			Payload: map[string]any{"text": "hello"},
		},
		{
			ID:      "point2",
			Vector:  []float32{0.4, 0.5, 0.6},
			Payload: map[string]any{"text": "world"},
		},
	}
	err := store.Upsert(context.Background(), "test_collection", points)

	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify request body
	pointsData, ok := receivedBody["points"].([]any)
	if !ok {
		t.Fatal("expected points array in request")
	}
	if len(pointsData) != 2 {
		t.Errorf("expected 2 points, got %d", len(pointsData))
	}
}

func TestQdrantStore_Search_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/search") {
			t.Errorf("expected /search in path, got %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		// Verify request
		if body["limit"] != float64(5) {
			t.Errorf("expected limit=5, got %v", body["limit"])
		}
		if body["with_payload"] != true {
			t.Error("expected with_payload=true")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]any{
				{"id": "1", "score": 0.95, "payload": map[string]any{"text": "result1"}},
				{"id": "2", "score": 0.85, "payload": map[string]any{"text": "result2"}},
			},
		})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	results, err := store.Search(context.Background(), SearchParams{
		Collection: "test_collection",
		Vector:     []float32{0.1, 0.2, 0.3},
		Limit:      5,
	})

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	if results[0].ID != "1" {
		t.Errorf("expected first ID=1, got %s", results[0].ID)
	}
	if results[0].Score != 0.95 {
		t.Errorf("expected first score=0.95, got %f", results[0].Score)
	}
	if results[0].Payload["text"] != "result1" {
		t.Errorf("expected first payload text=result1, got %v", results[0].Payload["text"])
	}
}

func TestQdrantStore_Search_WithFilter(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	store.Search(context.Background(), SearchParams{
		Collection: "test_collection",
		Vector:     []float32{0.1, 0.2, 0.3},
		Limit:      5,
		Filter: &Filter{
			Must: []Condition{
				{Field: "thread_id", Match: "abc123"},
			},
		},
	})

	// Verify filter in request
	filter, ok := receivedBody["filter"].(map[string]any)
	if !ok {
		t.Fatal("expected filter in request")
	}
	must, ok := filter["must"].([]any)
	if !ok || len(must) != 1 {
		t.Fatal("expected must conditions in filter")
	}
}

func TestQdrantStore_Search_WithScoreThreshold(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	store.Search(context.Background(), SearchParams{
		Collection:     "test_collection",
		Vector:         []float32{0.1, 0.2, 0.3},
		Limit:          5,
		ScoreThreshold: 0.5,
	})

	if receivedBody["score_threshold"] != 0.5 {
		t.Errorf("expected score_threshold=0.5, got %v", receivedBody["score_threshold"])
	}
}

func TestQdrantStore_Search_NoResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	results, err := store.Search(context.Background(), SearchParams{
		Collection: "test_collection",
		Vector:     []float32{0.1, 0.2, 0.3},
		Limit:      5,
	})

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestQdrantStore_Search_NumericID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]any{
				{"id": float64(12345), "score": 0.9, "payload": map[string]any{}},
			},
		})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	results, err := store.Search(context.Background(), SearchParams{
		Collection: "test_collection",
		Vector:     []float32{0.1, 0.2, 0.3},
		Limit:      5,
	})

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "12345" {
		t.Errorf("expected ID=12345, got %s", results[0].ID)
	}
}

func TestQdrantStore_Delete_Success(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/delete") {
			t.Errorf("expected /delete in path, got %s", r.URL.Path)
		}

		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	err := store.Delete(context.Background(), "test_collection", []string{"id1", "id2"})

	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	points, ok := receivedBody["points"].([]any)
	if !ok || len(points) != 2 {
		t.Error("expected 2 point IDs in request")
	}
}

func TestQdrantStore_ConnectionError(t *testing.T) {
	store := NewQdrantStore(QdrantConfig{
		BaseURL: "http://localhost:1",
		Timeout: 100 * time.Millisecond,
	})

	err := store.CreateCollection(context.Background(), "test", 768)

	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestQdrantStore_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.CreateCollection(ctx, "test", 768)

	if err == nil {
		t.Fatal("expected context canceled error")
	}
}

// Benchmark
func BenchmarkQdrantStore_Search(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]any{
				{"id": "1", "score": 0.9, "payload": map[string]any{}},
			},
		})
	}))
	defer server.Close()

	store := NewQdrantStore(QdrantConfig{BaseURL: server.URL})
	ctx := context.Background()
	params := SearchParams{
		Collection: "test",
		Vector:     make([]float32, 768),
		Limit:      5,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Search(ctx, params)
	}
}
