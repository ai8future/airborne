package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFileStore_RetriesTransientFailure_GetStore(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodGet)
		}
		if got := r.URL.Path; got != "/fileSearchStores/test-store" {
			t.Fatalf("path = %s, want /fileSearchStores/test-store", got)
		}

		call := calls.Add(1)
		if call == 1 {
			http.Error(w, "temporary unavailable", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fileSearchStoreResponse{
			Name:               "fileSearchStores/test-store",
			DisplayName:        "Test Store",
			CreateTime:         "2026-07-04T00:00:00Z",
			TotalDocumentCount: 1,
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	result, err := GetFileSearchStore(context.Background(), FileStoreConfig{APIKey: "test-key", BaseURL: srv.URL}, "test-store")
	if err != nil {
		t.Fatalf("GetFileSearchStore() error = %v", err)
	}
	if result == nil {
		t.Fatal("GetFileSearchStore() result is nil")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}

func TestFileStore_EscapesStoreIDAndAPIKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/fileSearchStores/store%2F..%2Fevil%3Fforce=true" {
			t.Fatalf("escaped path = %s, want escaped store id", got)
		}
		if got := r.URL.Query().Get("key"); got != "test-key&force=true" {
			t.Fatalf("key query = %q, want exact API key value", got)
		}
		if got := r.URL.Query().Get("force"); got != "" {
			t.Fatalf("unexpected query injection force=%q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fileSearchStoreResponse{
			Name:               "fileSearchStores/store",
			DisplayName:        "Escaped Store",
			CreateTime:         "2026-07-04T00:00:00Z",
			TotalDocumentCount: 1,
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	_, err := GetFileSearchStore(
		context.Background(),
		FileStoreConfig{APIKey: "test-key&force=true", BaseURL: srv.URL},
		"store/../evil?force=true",
	)
	if err != nil {
		t.Fatalf("GetFileSearchStore() error = %v", err)
	}
}

func TestFileStore_RetriesTransientFailure_CreateStoreRewindsBody(t *testing.T) {
	t.Parallel()

	const displayName = "Retry Store"
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if got := r.URL.Path; got != "/fileSearchStores" {
			t.Fatalf("path = %s, want /fileSearchStores", got)
		}

		call := calls.Add(1)
		if call == 1 {
			http.Error(w, "temporary unavailable", http.StatusServiceUnavailable)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), `"displayName":"`+displayName+`"`) {
			t.Fatalf("second request body = %q, want displayName payload", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fileSearchStoreResponse{
			Name:               "fileSearchStores/retry-store",
			DisplayName:        displayName,
			CreateTime:         "2026-07-04T00:00:00Z",
			TotalDocumentCount: 0,
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	result, err := CreateFileSearchStore(context.Background(), FileStoreConfig{APIKey: "test-key", BaseURL: srv.URL}, displayName)
	if err != nil {
		t.Fatalf("CreateFileSearchStore() error = %v", err)
	}
	if result == nil {
		t.Fatal("CreateFileSearchStore() result is nil")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}
