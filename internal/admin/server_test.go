package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai8future/airborne/internal/db"
)

func TestDetectMIMEType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"document.pdf", "application/pdf"},
		{"readme.txt", "text/plain"},
		{"notes.md", "text/markdown"},
		{"data.csv", "text/csv"},
		{"config.json", "application/json"},
		{"page.html", "text/html"},
		{"photo.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"animation.gif", "image/gif"},
		{"image.webp", "image/webp"},
		{"icon.svg", "image/svg+xml"},
		{"audio.mp3", "audio/mpeg"},
		{"sound.wav", "audio/wav"},
		{"video.mp4", "video/mp4"},
		{"clip.webm", "video/webm"},
		{"report.doc", "application/msword"},
		{"report.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"data.xls", "application/vnd.ms-excel"},
		{"data.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"slides.ppt", "application/vnd.ms-powerpoint"},
		{"slides.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		// Case insensitive
		{"PHOTO.PNG", "image/png"},
		{"Doc.PDF", "application/pdf"},
		// Unknown extension
		{"binary.xyz", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := detectMIMEType(tt.filename)
			if got != tt.want {
				t.Errorf("detectMIMEType(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestBuildCompressedHistory_Empty(t *testing.T) {
	var prev string
	result := buildCompressedHistory(nil, &prev)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestBuildCompressedHistory_BasicMessages(t *testing.T) {
	messages := []db.Message{
		{Role: "user", Content: "Hello", CreatedAt: time.Now()},
		{Role: "assistant", Content: "Hi there", CreatedAt: time.Now()},
		{Role: "user", Content: "How are you?", CreatedAt: time.Now()},
	}

	var prev string
	result := buildCompressedHistory(messages, &prev)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0].Role != "user" || result[0].Content != "Hello" {
		t.Errorf("first message = %+v, want user/Hello", result[0])
	}
	if result[1].Role != "assistant" || result[1].Content != "Hi there" {
		t.Errorf("second message = %+v, want assistant/Hi there", result[1])
	}
}

func TestBuildCompressedHistory_SkipsEmptyContent(t *testing.T) {
	messages := []db.Message{
		{Role: "user", Content: "Hello", CreatedAt: time.Now()},
		{Role: "assistant", Content: "  ", CreatedAt: time.Now()}, // whitespace-only
		{Role: "user", Content: "Follow up", CreatedAt: time.Now()},
	}

	var prev string
	result := buildCompressedHistory(messages, &prev)
	if len(result) != 2 {
		t.Errorf("expected 2 messages (skipping empty), got %d", len(result))
	}
}

func TestBuildCompressedHistory_TracksResponseID(t *testing.T) {
	respID := "resp-123"
	messages := []db.Message{
		{Role: "user", Content: "Hello", CreatedAt: time.Now()},
		{Role: "assistant", Content: "Hi", ResponseID: &respID, CreatedAt: time.Now()},
	}

	var prev string
	buildCompressedHistory(messages, &prev)
	if prev != "resp-123" {
		t.Errorf("previousResponseID = %q, want %q", prev, "resp-123")
	}
}

func TestBuildCompressedHistory_DropsAIResponsesWhenTooMany(t *testing.T) {
	// Create > dropAIResponsesLimit (6) AI responses
	var messages []db.Message
	for i := 0; i < 8; i++ {
		messages = append(messages, db.Message{
			Role: "user", Content: "Question " + string(rune('A'+i)), CreatedAt: time.Now(),
		})
		messages = append(messages, db.Message{
			Role: "assistant", Content: "Answer " + string(rune('A'+i)), CreatedAt: time.Now(),
		})
	}

	var prev string
	result := buildCompressedHistory(messages, &prev)

	// When > 6 AI responses, all assistant messages should be dropped
	for _, msg := range result {
		if msg.Role == "assistant" {
			t.Error("expected no assistant messages when count exceeds dropAIResponsesLimit")
			break
		}
	}
}

func TestHandleHealth_NoDB(t *testing.T) {
	s := &Server{
		dbClient: nil,
		version: VersionInfo{
			Version:   "1.0.0-test",
			GitCommit: "abc123",
			BuildTime: "2026-03-21T00:00:00Z",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("status = %q, want %q", resp["status"], "healthy")
	}
	if resp["database"] != "not_configured" {
		t.Errorf("database = %q, want %q", resp["database"], "not_configured")
	}
}

func TestHandleVersion(t *testing.T) {
	s := &Server{
		version: VersionInfo{
			Version:   "1.0.0-test",
			GitCommit: "abc123",
			BuildTime: "2026-03-21T00:00:00Z",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/version", nil)
	w := httptest.NewRecorder()

	s.handleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp VersionInfo
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if resp.Version != "1.0.0-test" {
		t.Errorf("Version = %q, want %q", resp.Version, "1.0.0-test")
	}
	if resp.GitCommit != "abc123" {
		t.Errorf("GitCommit = %q, want %q", resp.GitCommit, "abc123")
	}
}

func TestHandleActivity_NoDB(t *testing.T) {
	s := &Server{dbClient: nil}

	req := httptest.NewRequest(http.MethodGet, "/admin/activity?limit=10", nil)
	w := httptest.NewRecorder()

	s.handleActivity(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if resp["error"] != "database not configured" {
		t.Errorf("error = %q, want %q", resp["error"], "database not configured")
	}
}

func TestHandleDebug_MissingID(t *testing.T) {
	s := &Server{dbClient: nil}

	req := httptest.NewRequest(http.MethodGet, "/admin/debug/", nil)
	w := httptest.NewRecorder()

	s.handleDebug(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDebug_InvalidUUID(t *testing.T) {
	s := &Server{dbClient: nil}

	req := httptest.NewRequest(http.MethodGet, "/admin/debug/not-a-uuid", nil)
	w := httptest.NewRecorder()

	s.handleDebug(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDebug_NoDB(t *testing.T) {
	s := &Server{dbClient: nil}

	req := httptest.NewRequest(http.MethodGet, "/admin/debug/550e8400-e29b-41d4-a716-446655440000", nil)
	w := httptest.NewRecorder()

	s.handleDebug(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
