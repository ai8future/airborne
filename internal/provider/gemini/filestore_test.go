package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestDefaultFileStoreRequestTimeout(t *testing.T) {
	if defaultFileStoreRequestTimeout != 120*time.Second {
		t.Fatalf("default file-store request timeout = %s, want 120s", defaultFileStoreRequestTimeout)
	}

	const testTimeout = 50 * time.Millisecond
	requestStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now()
	_, err = newFileStoreClient(testTimeout).Do(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("request error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed < testTimeout/2 || elapsed > time.Second {
		t.Fatalf("configured timeout elapsed in %s, want approximately %s", elapsed, testTimeout)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("request never reached the server")
	}
}

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
func TestFileStoreHelperContracts(t *testing.T) {
	if !isOfficeFile("text/csv") || isOfficeFile("text/plain") {
		t.Fatal("office types")
	}
	if (FileStoreConfig{}).getBaseURL() == "" || (FileStoreConfig{BaseURL: "http://x"}).getBaseURL() != "http://x" {
		t.Fatal("base")
	}
	if got := escapedResourcePath("/a b/c/"); got != "a%20b/c" {
		t.Fatal(got)
	}
	if got := fileSearchStoreResource("a/b", ":import"); got != "fileSearchStores/a%2Fb:import" {
		t.Fatal(got)
	}
	if !strings.Contains(geminiURLWithKey("http://x", "k", "p", map[string]string{"x": "y"}), "key=k") {
		t.Fatal("url")
	}
	if readGeminiErrorBody(strings.NewReader("x")) != "x" {
		t.Fatal("body")
	}
}

func TestFileSearchStoreInputValidation(t *testing.T) {
	ctx := context.Background()
	invalidURL := FileStoreConfig{APIKey: "key", BaseURL: "file:///tmp/not-an-api"}

	if _, err := CreateFileSearchStore(ctx, FileStoreConfig{}, "store"); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("CreateFileSearchStore missing key error = %v", err)
	}
	if _, err := CreateFileSearchStore(ctx, invalidURL, "store"); err == nil || !strings.Contains(err.Error(), "invalid base URL") {
		t.Errorf("CreateFileSearchStore invalid URL error = %v", err)
	}

	if _, err := UploadFileToFileSearchStore(ctx, FileStoreConfig{}, "store", "file.txt", "text/plain", strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("UploadFileToFileSearchStore missing key error = %v", err)
	}
	if _, err := UploadFileToFileSearchStore(ctx, FileStoreConfig{APIKey: "key"}, " ", "file.txt", "text/plain", strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "store ID") {
		t.Errorf("UploadFileToFileSearchStore missing store error = %v", err)
	}
	if _, err := UploadFileToFileSearchStore(ctx, invalidURL, "store", "file.txt", "text/plain", strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "invalid base URL") {
		t.Errorf("UploadFileToFileSearchStore invalid URL error = %v", err)
	}
	readErr := errors.New("fixture read failure")
	if _, err := UploadFileToFileSearchStore(ctx, FileStoreConfig{APIKey: "key"}, "store", "file.txt", "text/plain", failingReader{err: readErr}); !errors.Is(err, readErr) {
		t.Errorf("UploadFileToFileSearchStore reader error = %v, want wrapped %v", err, readErr)
	}

	if err := DeleteFileSearchStore(ctx, FileStoreConfig{}, "store", false); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("DeleteFileSearchStore missing key error = %v", err)
	}
	if err := DeleteFileSearchStore(ctx, FileStoreConfig{APIKey: "key"}, " ", false); err == nil || !strings.Contains(err.Error(), "store ID") {
		t.Errorf("DeleteFileSearchStore missing store error = %v", err)
	}
	if err := DeleteFileSearchStore(ctx, invalidURL, "store", false); err == nil || !strings.Contains(err.Error(), "invalid base URL") {
		t.Errorf("DeleteFileSearchStore invalid URL error = %v", err)
	}

	if _, err := GetFileSearchStore(ctx, FileStoreConfig{}, "store"); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("GetFileSearchStore missing key error = %v", err)
	}
	if _, err := GetFileSearchStore(ctx, FileStoreConfig{APIKey: "key"}, " "); err == nil || !strings.Contains(err.Error(), "store ID") {
		t.Errorf("GetFileSearchStore missing store error = %v", err)
	}
	if _, err := GetFileSearchStore(ctx, invalidURL, "store"); err == nil || !strings.Contains(err.Error(), "invalid base URL") {
		t.Errorf("GetFileSearchStore invalid URL error = %v", err)
	}

	if _, err := ListFileSearchStores(ctx, FileStoreConfig{}, 10); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("ListFileSearchStores missing key error = %v", err)
	}
	if _, err := ListFileSearchStores(ctx, invalidURL, 10); err == nil || !strings.Contains(err.Error(), "invalid base URL") {
		t.Errorf("ListFileSearchStores invalid URL error = %v", err)
	}
}

func TestFileSearchStoreStatusAndListMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fileSearchStores/processing":
			_, _ = io.WriteString(w, `{"name":"fileSearchStores/processing","displayName":"Processing","createTime":"2026-01-01T00:00:00Z","totalDocumentCount":3,"processedDocumentCount":1}`)
		case "/fileSearchStores/partial":
			_, _ = io.WriteString(w, `{"name":"fileSearchStores/partial","displayName":"Partial","createTime":"bad-time","totalDocumentCount":3,"processedDocumentCount":1,"failedDocumentCount":1}`)
		case "/fileSearchStores":
			if got := r.URL.Query().Get("pageSize"); got != "" {
				t.Errorf("out-of-range limit should be omitted, pageSize=%q", got)
			}
			_, _ = io.WriteString(w, `{"fileSearchStores":[{"name":"raw-id","displayName":"Raw","createTime":"bad-time","totalDocumentCount":2},{"name":"fileSearchStores/nested-id","displayName":"Nested","createTime":"2026-01-01T00:00:00Z","totalDocumentCount":4}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := FileStoreConfig{APIKey: "key", BaseURL: srv.URL}
	processing, err := GetFileSearchStore(context.Background(), cfg, "processing")
	if err != nil || processing.Status != "processing" {
		t.Fatalf("processing store = %#v, %v", processing, err)
	}
	partial, err := GetFileSearchStore(context.Background(), cfg, "partial")
	if err != nil || partial.Status != "partial" || !partial.CreatedAt.IsZero() {
		t.Fatalf("partial store = %#v, %v", partial, err)
	}
	stores, err := ListFileSearchStores(context.Background(), cfg, 21)
	if err != nil || len(stores) != 2 {
		t.Fatalf("ListFileSearchStores() = %#v, %v", stores, err)
	}
	if stores[0].StoreID != "raw-id" || stores[1].StoreID != "nested-id" {
		t.Fatalf("store IDs = %#v", stores)
	}
}
func TestFileSearchStoreHTTPLifecycle(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "k" {
			t.Error("key")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/fileSearchStores" && r.Method == http.MethodGet {
			io.WriteString(w, `{"fileSearchStores":[{"name":"fileSearchStores/id","displayName":"n","createTime":"2026-01-01T00:00:00Z","totalDocumentCount":1}]}`)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(200)
			return
		}
		io.WriteString(w, `{"name":"fileSearchStores/id","displayName":"n","createTime":"2026-01-01T00:00:00Z","totalDocumentCount":1}`)
	}))
	defer s.Close()
	cfg := FileStoreConfig{APIKey: "k", BaseURL: s.URL}
	if _, e := CreateFileSearchStore(context.Background(), cfg, "n"); e != nil {
		t.Fatal(e)
	}
	if _, e := GetFileSearchStore(context.Background(), cfg, "id"); e != nil {
		t.Fatal(e)
	}
	if _, e := ListFileSearchStores(context.Background(), cfg, 1); e != nil {
		t.Fatal(e)
	}
	if e := DeleteFileSearchStore(context.Background(), cfg, "id", true); e != nil {
		t.Fatal(e)
	}
}

func TestFileSearchStoreDirectUploadAndPolling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "key value" {
			t.Errorf("API key = %q", r.URL.Query().Get("key"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/fileSearchStores/store id:uploadToFileSearchStore":
			if r.Header.Get("X-Goog-Upload-Protocol") != "raw" {
				t.Error("expected raw upload")
			}
			if body, _ := io.ReadAll(r.Body); string(body) != "contents" {
				t.Errorf("upload body = %q", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"operations/upload","response":{"name":"fileSearchStores/store id/files/file-1"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/operations/upload":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"operations/upload","done":true}`)
		default:
			http.Error(w, "unexpected endpoint: "+r.Method+" "+r.URL.EscapedPath(), http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := UploadFileToFileSearchStore(context.Background(), FileStoreConfig{APIKey: "key value", BaseURL: srv.URL}, "store id", "note.txt", "text/plain", strings.NewReader("contents"))
	if err != nil {
		t.Fatalf("UploadFileToFileSearchStore() error = %v", err)
	}
	if got.FileID != "file-1" || got.Status != "completed" || got.Operation != "operations/upload" {
		t.Fatalf("upload result = %#v", got)
	}
}

func TestWaitForOperationErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"operations/fail","done":true,"error":{"code":13,"message":"fixture failed"}}`)
	}))
	defer srv.Close()
	status, err := waitForOperation(context.Background(), FileStoreConfig{APIKey: "k", BaseURL: srv.URL}, "operations/fail")
	if status != "failed" || err == nil || !strings.Contains(err.Error(), "fixture failed") {
		t.Fatalf("wait result = %q, %v", status, err)
	}
}

func TestFilesAPIUploadInitiationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "init rejected", http.StatusBadRequest)
	}))
	defer srv.Close()
	_, err := uploadToFilesAPI(context.Background(), FileStoreConfig{APIKey: "k", BaseURL: srv.URL}, "bad.csv", "text/csv", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "init upload failed") || !strings.Contains(err.Error(), "init rejected") {
		t.Fatalf("error = %v", err)
	}
}

func TestFilesAPIUploadMissingResumableURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	_, err := uploadToFilesAPI(context.Background(), FileStoreConfig{APIKey: "k", BaseURL: srv.URL}, "x", "text/plain", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "no resumable upload URL") {
		t.Fatalf("error = %v", err)
	}
}

func TestFilesAPIUploadFinalizeError(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files" {
			w.Header().Set("X-Goog-Upload-URL", srv.URL+"/upload")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "finalize rejected", http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := uploadToFilesAPI(context.Background(), FileStoreConfig{APIKey: "k", BaseURL: srv.URL}, "x", "text/plain", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "upload failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestFileSearchStoreOfficeUploadLifecycle(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files":
			if r.Header.Get("X-Goog-Upload-Protocol") != "resumable" {
				t.Error("expected resumable protocol")
			}
			w.Header().Set("X-Goog-Upload-URL", srv.URL+"/resumable")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/resumable":
			if r.Header.Get("X-Goog-Upload-Command") != "upload, finalize" {
				t.Error("expected finalization")
			}
			_, _ = io.WriteString(w, `{"file":{"name":"files/f-1","displayName":"book.csv"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/fileSearchStores/store:importFile":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "files/f-1") {
				t.Errorf("import body = %s", body)
			}
			_, _ = io.WriteString(w, `{"name":"operations/import","response":{"name":"fileSearchStores/store/files/doc-1"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/operations/import":
			_, _ = io.WriteString(w, `{"name":"operations/import","done":true}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/files/f-1":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected endpoint: "+r.Method+" "+r.URL.EscapedPath(), http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := UploadFileToFileSearchStore(context.Background(), FileStoreConfig{APIKey: "k", BaseURL: srv.URL}, "store", "book.csv", "text/csv", strings.NewReader("a,b\n1,2\n"))
	if err != nil {
		t.Fatalf("office upload error = %v", err)
	}
	if got.FileID != "doc-1" || got.Status != "completed" {
		t.Fatalf("office result = %#v", got)
	}
}
