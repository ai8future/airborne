package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("http://localhost:8080")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "http://localhost:8080")
	}
	if c.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}
}

func TestClient_Health_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(HealthResponse{
			Status:   "healthy",
			Database: "healthy",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	health, err := c.Health()
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("Status = %q, want %q", health.Status, "healthy")
	}
	if health.Database != "healthy" {
		t.Errorf("Database = %q, want %q", health.Database, "healthy")
	}
}

func TestClient_Health_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Health()
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestClient_Health_ConnectionRefused(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // port 1 should refuse
	_, err := c.Health()
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestClient_Activity_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/activity" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "5" {
			t.Errorf("limit = %q, want %q", r.URL.Query().Get("limit"), "5")
		}
		if r.URL.Query().Get("tenant_id") != "ai8" {
			t.Errorf("tenant_id = %q, want %q", r.URL.Query().Get("tenant_id"), "ai8")
		}
		json.NewEncoder(w).Encode(ActivityResponse{
			Activity: []Activity{
				{ID: "msg-1", Model: "gpt-4", Status: "success"},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Activity(5, "ai8")
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if len(resp.Activity) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(resp.Activity))
	}
	if resp.Activity[0].Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", resp.Activity[0].Model, "gpt-4")
	}
}

func TestClient_Activity_NoTenant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tenant_id") != "" {
			t.Error("tenant_id should not be set when empty")
		}
		json.NewEncoder(w).Encode(ActivityResponse{Activity: []Activity{}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Activity(10, "")
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if len(resp.Activity) != 0 {
		t.Errorf("expected empty activity, got %d", len(resp.Activity))
	}
}

func TestClient_Debug_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/debug/msg-123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(DebugResponse{
			MessageID: "msg-123",
			Status:    "success",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Debug("msg-123")
	if err != nil {
		t.Fatalf("Debug() error = %v", err)
	}
	if resp.MessageID != "msg-123" {
		t.Errorf("MessageID = %q, want %q", resp.MessageID, "msg-123")
	}
}

func TestClient_Thread_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/thread/thread-456" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ThreadResponse{
			ThreadID: "thread-456",
			Messages: []ThreadMessage{
				{ID: "msg-1", Role: "user", Content: "Hello"},
				{ID: "msg-2", Role: "assistant", Content: "Hi there"},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Thread("thread-456")
	if err != nil {
		t.Fatalf("Thread() error = %v", err)
	}
	if resp.ThreadID != "thread-456" {
		t.Errorf("ThreadID = %q, want %q", resp.ThreadID, "thread-456")
	}
	if len(resp.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(resp.Messages))
	}
}

func TestClient_Test_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var req TestRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Prompt != "Hello" {
			t.Errorf("Prompt = %q, want %q", req.Prompt, "Hello")
		}
		json.NewEncoder(w).Encode(TestResponse{
			Reply:    "Hi there",
			Provider: "gemini",
			Model:    "gemini-pro",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	resp, err := c.Test(TestRequest{Prompt: "Hello", TenantID: "ai8"})
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	if resp.Reply != "Hi there" {
		t.Errorf("Reply = %q, want %q", resp.Reply, "Hi there")
	}
	if resp.Provider != "gemini" {
		t.Errorf("Provider = %q, want %q", resp.Provider, "gemini")
	}
}

func TestClient_Test_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Test(TestRequest{Prompt: "Hello"})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}
