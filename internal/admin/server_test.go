package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chassis "github.com/ai8future/chassis-go/v11"
	"github.com/ai8future/chassis-go/v11/guard"
	"github.com/ai8future/chassis-go/v11/registry"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/db"
	"google.golang.org/grpc"
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

func TestAdminRequestTimeoutPolicy(t *testing.T) {
	tests := []struct {
		name string
		path string
		want time.Duration
	}{
		{name: "ordinary admin route", path: "/admin/activity", want: defaultAdminRequestTimeout},
		{name: "unknown admin route", path: "/admin/future", want: defaultAdminRequestTimeout},
		{name: "chat route", path: "/admin/chat", want: adminLLMRequestTimeout},
		{name: "test route", path: "/admin/test", want: adminLLMRequestTimeout},
		{name: "upload route", path: "/admin/upload", want: adminUploadRequestTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got time.Duration
			handler := adminRequestTimeout(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				deadline, ok := r.Context().Deadline()
				if !ok {
					t.Fatal("request context has no deadline")
				}
				got = time.Until(deadline)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tt.path, nil))
			if delta := got - tt.want; delta < -time.Second || delta > time.Second {
				t.Fatalf("request timeout = %s, want approximately %s", got, tt.want)
			}
		})
	}
}

func TestAdminRequestTimeoutPolicyFitsWriteTimeout(t *testing.T) {
	if adminLLMRequestTimeout >= adminServerWriteTimeout {
		t.Fatalf("maximum request timeout %s must be less than write timeout %s", adminLLMRequestTimeout, adminServerWriteTimeout)
	}
}

func TestNewServerInstallsAdminRequestTimeout(t *testing.T) {
	if err := registry.Init(func() {}, chassis.Version); err != nil {
		t.Fatalf("initialize chassis registry: %v", err)
	}
	t.Cleanup(func() { registry.Shutdown("admin timeout test complete") })

	var got time.Duration
	s := NewServer(nil, Config{
		HealthChecks: map[string]func(context.Context) error{
			"deadline": func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Error("health check context has no deadline")
					return nil
				}
				got = time.Until(deadline)
				return nil
			},
		},
	})

	s.server.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/healthz", nil))
	if delta := got - defaultAdminRequestTimeout; delta < -time.Second || delta > time.Second {
		t.Fatalf("installed request timeout = %s, want approximately %s", got, defaultAdminRequestTimeout)
	}
}

func TestBuildCompressedHistory_Empty(t *testing.T) {
	var prev string
	result := buildCompressedHistory(nil, &prev)
	if len(result) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result))
	}
}

func TestCapHistoryKeepsMostRecentMessages(t *testing.T) {
	branch := []db.ChatMessage{{Role: "one"}, {Role: "two"}, {Role: "three"}}
	got := capHistory(branch, 2)
	if len(got) != 2 || got[0].Role != "two" || got[1].Role != "three" {
		t.Fatalf("capHistory = %#v, want the two newest messages", got)
	}
	if got := capHistory(branch, len(branch)); len(got) != len(branch) {
		t.Fatalf("equal cap returned %d messages, want %d", len(got), len(branch))
	}
}

func TestMessageContentText(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"empty", nil, ""},
		{"text object", json.RawMessage(` { "text": "hello" } `), "hello"},
		{"bare string", json.RawMessage(`"hello"`), "hello"},
		{"object without text", json.RawMessage(`{"kind":"other"}`), `{"kind":"other"}`},
		{"invalid json", json.RawMessage(`not-json`), "not-json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := messageContentText(test.raw); got != test.want {
				t.Fatalf("messageContentText(%s) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestBuildCompressedHistory_BasicMessages(t *testing.T) {
	messages := []db.ChatMessage{
		{Role: "user", Content: db.TextContent("Hello"), CreatedAt: time.Now()},
		{Role: "assistant", Content: db.TextContent("Hi there"), CreatedAt: time.Now()},
		{Role: "user", Content: db.TextContent("How are you?"), CreatedAt: time.Now()},
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
	messages := []db.ChatMessage{
		{Role: "user", Content: db.TextContent("Hello"), CreatedAt: time.Now()},
		{Role: "assistant", Content: db.TextContent("  "), CreatedAt: time.Now()}, // whitespace-only
		{Role: "user", Content: db.TextContent("Follow up"), CreatedAt: time.Now()},
	}

	var prev string
	result := buildCompressedHistory(messages, &prev)
	if len(result) != 2 {
		t.Errorf("expected 2 messages (skipping empty), got %d", len(result))
	}
}

func TestBuildCompressedHistory_TracksResponseID(t *testing.T) {
	respID := "resp-123"
	messages := []db.ChatMessage{
		{Role: "user", Content: db.TextContent("Hello"), CreatedAt: time.Now()},
		{Role: "assistant", Content: db.TextContent("Hi"), ResponseID: &respID, CreatedAt: time.Now()},
	}

	var prev string
	buildCompressedHistory(messages, &prev)
	if prev != "resp-123" {
		t.Errorf("previousResponseID = %q, want %q", prev, "resp-123")
	}
}

func TestBuildCompressedHistory_DropsAIResponsesWhenTooMany(t *testing.T) {
	// Create > dropAIResponsesLimit (6) AI responses
	var messages []db.ChatMessage
	for i := 0; i < 8; i++ {
		messages = append(messages, db.ChatMessage{
			Role: "user", Content: db.TextContent("Question " + string(rune('A'+i))), CreatedAt: time.Now(),
		})
		messages = append(messages, db.ChatMessage{
			Role: "assistant", Content: db.TextContent("Answer " + string(rune('A'+i))), CreatedAt: time.Now(),
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

func TestHealthAndVersionRejectNonGET(t *testing.T) {
	s := &Server{version: VersionInfo{Version: "test"}}
	for name, handler := range map[string]http.HandlerFunc{
		"health":  s.handleHealth,
		"version": s.handleVersion,
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest(http.MethodPost, "/admin/"+name, nil))
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestHandleVersionResponseContract(t *testing.T) {
	s := &Server{version: VersionInfo{Version: "1.2.3", GitCommit: "abc", BuildTime: "now"}}
	w := httptest.NewRecorder()
	s.handleVersion(w, httptest.NewRequest(http.MethodGet, "/admin/version", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status/content-type = %d/%q", w.Code, w.Header().Get("Content-Type"))
	}
	var got VersionInfo
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != s.version {
		t.Fatalf("version = %#v, want %#v", got, s.version)
	}
}

func TestHandleDebugValidationAndNoDatabase(t *testing.T) {
	s := &Server{}
	for _, test := range []struct {
		name   string
		method string
		path   string
		status int
		error  string
	}{
		{"method", http.MethodPost, "/admin/debug/id", http.StatusMethodNotAllowed, ""},
		{"missing id", http.MethodGet, "/admin/debug/", http.StatusBadRequest, "message_id required"},
		{"invalid id", http.MethodGet, "/admin/debug/nope", http.StatusBadRequest, "invalid message_id format"},
		{"no database", http.MethodGet, "/admin/debug/8e67ec2c-3f3a-4b1a-9f21-8ad9bd298c3a", http.StatusServiceUnavailable, "database not configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.handleDebug(w, httptest.NewRequest(test.method, test.path, nil))
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, test.status, w.Body.String())
			}
			if test.error != "" && !strings.Contains(w.Body.String(), test.error) {
				t.Fatalf("body %q lacks %q", w.Body.String(), test.error)
			}
		})
	}
}

func TestHandleThreadValidationAndNoDatabase(t *testing.T) {
	s := &Server{}
	for _, test := range []struct {
		name   string
		method string
		path   string
		status int
		error  string
	}{
		{"method", http.MethodPost, "/admin/thread/id", http.StatusMethodNotAllowed, ""},
		{"missing id", http.MethodGet, "/admin/thread/", http.StatusBadRequest, "thread_id required"},
		{"invalid id", http.MethodGet, "/admin/thread/nope", http.StatusBadRequest, "invalid thread_id format"},
		{"no database", http.MethodGet, "/admin/thread/8e67ec2c-3f3a-4b1a-9f21-8ad9bd298c3a", http.StatusServiceUnavailable, "database not configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.handleThread(w, httptest.NewRequest(test.method, test.path, nil))
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, test.status, w.Body.String())
			}
			if test.error != "" && !strings.Contains(w.Body.String(), test.error) {
				t.Fatalf("body %q lacks %q", w.Body.String(), test.error)
			}
		})
	}
}

func TestGetGRPCClientRequiresAddress(t *testing.T) {
	s := &Server{}
	if client, err := s.getGRPCClient(); err == nil || client != nil {
		t.Fatalf("getGRPCClient() = %#v, %v; want nil and configuration error", client, err)
	}
}

func TestHandleTestValidationAndUnavailableGRPC(t *testing.T) {
	s := &Server{}
	for _, test := range []struct {
		name     string
		method   string
		body     string
		status   int
		contains string
	}{
		{"method", http.MethodGet, "", http.StatusMethodNotAllowed, ""},
		{"invalid json", http.MethodPost, "{", http.StatusBadRequest, ""},
		{"missing prompt", http.MethodPost, `{}`, http.StatusBadRequest, "prompt is required"},
		{"unavailable grpc", http.MethodPost, `{"prompt":"hello"}`, http.StatusServiceUnavailable, "gRPC address not configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.handleTest(w, httptest.NewRequest(test.method, "/admin/test", strings.NewReader(test.body)))
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, test.status, w.Body.String())
			}
			if test.contains != "" && !strings.Contains(w.Body.String(), test.contains) {
				t.Fatalf("body %q lacks %q", w.Body.String(), test.contains)
			}
		})
	}
}

func TestHandleChatRequestValidation(t *testing.T) {
	s := &Server{}
	validThread := "8e67ec2c-3f3a-4b1a-9f21-8ad9bd298c3a"
	for _, test := range []struct {
		name     string
		method   string
		body     string
		status   int
		contains string
	}{
		{"method", http.MethodGet, "", http.StatusMethodNotAllowed, ""},
		{"invalid json", http.MethodPost, "{", http.StatusBadRequest, ""},
		{"missing message", http.MethodPost, `{}`, http.StatusBadRequest, "message is required"},
		{"missing thread", http.MethodPost, `{"message":"hello"}`, http.StatusBadRequest, "thread_id is required"},
		{"invalid thread", http.MethodPost, `{"message":"hello","thread_id":"nope"}`, http.StatusBadRequest, "invalid thread_id format"},
		{"invalid request id", http.MethodPost, `{"message":"hello","thread_id":"` + validThread + `","request_id":"bad id"}`, http.StatusBadRequest, "invalid request_id format"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.handleChat(w, httptest.NewRequest(test.method, "/admin/chat", strings.NewReader(test.body)))
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, test.status, w.Body.String())
			}
			if test.contains != "" && !strings.Contains(w.Body.String(), test.contains) {
				t.Fatalf("body %q lacks %q", w.Body.String(), test.contains)
			}
		})
	}
}

func TestAdminHTTPAuthMiddleware_PublicHealthNoToken(t *testing.T) {
	s := &Server{authToken: "secret-token"}
	handler := s.requireHTTPAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestAdminHTTPAuthMiddleware_RejectsProtectedWithoutToken(t *testing.T) {
	s := &Server{authToken: "secret-token"}
	handler := s.requireHTTPAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestAdminHTTPAuthMiddleware_RejectsProtectedWithWrongToken(t *testing.T) {
	s := &Server{authToken: "secret-token"}
	handler := s.requireHTTPAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestAdminHTTPAuthMiddleware_AllowsProtectedWithBearerToken(t *testing.T) {
	s := &Server{authToken: "secret-token"}
	handler := s.requireHTTPAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "reached"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp["status"] != "reached" {
		t.Fatalf("expected authenticated request to reach handler, got body=%v", resp)
	}
}

func TestAdminHTTP_CORSUsesConfiguredOrigins(t *testing.T) {
	handler := guard.CORS(guard.CORSConfig{
		AllowOrigins: adminAllowedOrigins([]string{"https://dashboard.example.com"}),
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key"},
		MaxAge:       10 * time.Minute,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/admin/activity", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unconfigured origin got CORS allow header %q", got)
	}

	req = httptest.NewRequest(http.MethodOptions, "/admin/activity", nil)
	req.Header.Set("Origin", "https://dashboard.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://dashboard.example.com" {
		t.Fatalf("configured origin CORS allow header = %q, want %q", got, "https://dashboard.example.com")
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

func TestHandleChat_InvalidRequestID(t *testing.T) {
	s := &Server{}

	req := httptest.NewRequest(http.MethodPost, "/admin/chat", strings.NewReader(`{
		"thread_id": "550e8400-e29b-41d4-a716-446655440000",
		"message": "hello",
		"request_id": "bad<script>"
	}`))
	w := httptest.NewRecorder()

	s.handleChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	var resp ChatResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Error != "invalid request_id format" {
		t.Fatalf("error = %q, want invalid request_id format", resp.Error)
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

type recordingAirborneClient struct {
	pb.AirborneServiceClient
	response *pb.GenerateReplyResponse
	err      error
	requests []*pb.GenerateReplyRequest
}

func (c *recordingAirborneClient) GenerateReply(_ context.Context, req *pb.GenerateReplyRequest, _ ...grpc.CallOption) (*pb.GenerateReplyResponse, error) {
	c.requests = append(c.requests, req)
	return c.response, c.err
}

func TestHandleTestAndChatSuccessUseRecordedGRPCRequest(t *testing.T) {
	client := &recordingAirborneClient{response: &pb.GenerateReplyResponse{
		ResponseId: "response-1",
		Text:       "hello from fake",
		Provider:   pb.Provider_PROVIDER_GEMINI,
		Model:      "gemini-test",
		Usage:      &pb.Usage{InputTokens: 3, OutputTokens: 5},
	}}
	s := &Server{grpcClient: client, authToken: "secret"}

	testRecorder := httptest.NewRecorder()
	s.handleTest(testRecorder, httptest.NewRequest(http.MethodPost, "/admin/test", strings.NewReader(`{"prompt":"test prompt","tenant_id":"tenant-a","provider":"openai"}`)))
	if testRecorder.Code != http.StatusOK || !strings.Contains(testRecorder.Body.String(), `"reply":"hello from fake"`) {
		t.Fatalf("test response = %d %s", testRecorder.Code, testRecorder.Body.String())
	}
	if len(client.requests) != 1 || client.requests[0].PreferredProvider != pb.Provider_PROVIDER_OPENAI || client.requests[0].ClientId != "dashboard-test" {
		t.Fatalf("test request = %#v", client.requests)
	}

	threadID := "a28a7a8c-464c-4ec7-a965-5b7f466608d8"
	chatRecorder := httptest.NewRecorder()
	s.handleChat(chatRecorder, httptest.NewRequest(http.MethodPost, "/admin/chat", strings.NewReader(`{"thread_id":"`+threadID+`","message":"continue","tenant_id":"tenant-a"}`)))
	if chatRecorder.Code != http.StatusOK || !strings.Contains(chatRecorder.Body.String(), `"content":"hello from fake"`) {
		t.Fatalf("chat response = %d %s", chatRecorder.Code, chatRecorder.Body.String())
	}
	if len(client.requests) != 2 || client.requests[1].RequestId != threadID || client.requests[1].ClientId != "dashboard-chat" {
		t.Fatalf("chat request = %#v", client.requests)
	}
}

func TestHandleUploadValidationAndUnavailableTenantManager(t *testing.T) {
	s := &Server{}

	method := httptest.NewRecorder()
	s.handleUpload(method, httptest.NewRequest(http.MethodGet, "/admin/upload", nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET upload status = %d", method.Code)
	}

	missing := httptest.NewRecorder()
	missingReq := httptest.NewRequest(http.MethodPost, "/admin/upload", strings.NewReader("--not-a-form"))
	missingReq.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	s.handleUpload(missing, missingReq)
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "failed to parse multipart form") {
		t.Fatalf("missing upload response = %d %s", missing.Code, missing.Body.String())
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	unavailable := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	s.handleUpload(unavailable, req)
	if unavailable.Code != http.StatusBadRequest || !strings.Contains(unavailable.Body.String(), "tenant manager not configured") {
		t.Fatalf("unavailable upload response = %d %s", unavailable.Code, unavailable.Body.String())
	}
}

func TestHandleChatWithFileRejectsInvalidThreadAndUnavailableTenant(t *testing.T) {
	s := &Server{}
	invalid := httptest.NewRecorder()
	s.handleChatWithFile(invalid, httptest.NewRequest(http.MethodPost, "/admin/chat", nil), ChatWithFileRequest{ThreadID: "not-a-uuid"})
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "invalid thread_id") {
		t.Fatalf("invalid file chat = %d %s", invalid.Code, invalid.Body.String())
	}

	unavailable := httptest.NewRecorder()
	s.handleChatWithFile(unavailable, httptest.NewRequest(http.MethodPost, "/admin/chat", nil), ChatWithFileRequest{ThreadID: "a28a7a8c-464c-4ec7-a965-5b7f466608d8", TenantID: "tenant-a"})
	if unavailable.Code != http.StatusBadRequest || !strings.Contains(unavailable.Body.String(), "tenant manager not configured") {
		t.Fatalf("unavailable file chat = %d %s", unavailable.Code, unavailable.Body.String())
	}
}

func TestAdminHTTPAuthAndTokenNormalization(t *testing.T) {
	s := &Server{authToken: "expected-token"}
	called := false
	handler := s.requireHTTPAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) }))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/admin/activity", nil))
	if unauthorized.Code != http.StatusUnauthorized || called || unauthorized.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthorized response = %d called=%t", unauthorized.Code, called)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/admin/activity", nil)
	authorizedReq.Header.Set("Authorization", "Bearer expected-token")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedReq)
	if authorized.Code != http.StatusNoContent || !called {
		t.Fatalf("authorized response = %d called=%t", authorized.Code, called)
	}

	public := httptest.NewRecorder()
	handler.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/admin/health", nil))
	if public.Code != http.StatusNoContent {
		t.Fatalf("public health = %d", public.Code)
	}
	if got := normalizeHTTPAuthHeader(" bearer  token "); got != "token" {
		t.Fatalf("normalized token = %q", got)
	}
	if !constantTimeTokenEqual("same", "same") || constantTimeTokenEqual("short", "longer") {
		t.Fatal("constant-time equality contract violated")
	}
}

func TestHandleActivityAndHealthWithoutDatabase(t *testing.T) {
	s := &Server{}
	activity := httptest.NewRecorder()
	s.handleActivity(activity, httptest.NewRequest(http.MethodGet, "/admin/activity?limit=invalid&tenant_id=tenant", nil))
	if activity.Code != http.StatusOK || !strings.Contains(activity.Body.String(), `"activity":[]`) {
		t.Fatalf("activity = %d %s", activity.Code, activity.Body.String())
	}
	health := httptest.NewRecorder()
	s.handleHealth(health, httptest.NewRequest(http.MethodGet, "/admin/health", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"database":"not_configured"`) {
		t.Fatalf("health = %d %s", health.Code, health.Body.String())
	}
}

func TestServerShutdownHandlesUnstartedHTTPServer(t *testing.T) {
	s := &Server{server: &http.Server{}}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown unstarted server: %v", err)
	}
}
