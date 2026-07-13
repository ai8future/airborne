package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
}

func TestDetectImageRequest(t *testing.T) {
	client := NewClient()

	tests := []struct {
		name       string
		text       string
		cfg        *Config
		wantPrompt string
		wantNil    bool
	}{
		{
			name:    "nil config",
			text:    "@image a sunset",
			cfg:     nil,
			wantNil: true,
		},
		{
			name:    "disabled config",
			text:    "@image a sunset",
			cfg:     &Config{Enabled: false, TriggerPhrases: []string{"@image"}},
			wantNil: true,
		},
		{
			name:    "empty trigger phrases",
			text:    "@image a sunset",
			cfg:     &Config{Enabled: true, TriggerPhrases: []string{}},
			wantNil: true,
		},
		{
			name:       "simple trigger",
			text:       "@image a sunset",
			cfg:        &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
			wantPrompt: "a sunset",
		},
		{
			name:       "case insensitive trigger",
			text:       "@IMAGE a sunset over mountains",
			cfg:        &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
			wantPrompt: "a sunset over mountains",
		},
		{
			name:       "multi-word trigger",
			text:       "generate image: a blue cat",
			cfg:        &Config{Enabled: true, TriggerPhrases: []string{"generate image:"}},
			wantPrompt: "a blue cat",
		},
		{
			name:       "trigger with leading/trailing spaces",
			text:       "  @image   flying car  ",
			cfg:        &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
			wantPrompt: "flying car",
		},
		{
			name:    "no matching trigger",
			text:    "hello world",
			cfg:     &Config{Enabled: true, TriggerPhrases: []string{"@image", "@picture"}},
			wantNil: true,
		},
		{
			name:    "trigger with empty prompt",
			text:    "@image   ",
			cfg:     &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
			wantNil: true,
		},
		{
			name:       "first matching trigger used",
			text:       "@picture a dog @image a cat",
			cfg:        &Config{Enabled: true, TriggerPhrases: []string{"@picture", "@image"}},
			wantPrompt: "a dog @image a cat",
		},
		{
			name:       "trigger in middle of text",
			text:       "Please @image a robot playing chess",
			cfg:        &Config{Enabled: true, TriggerPhrases: []string{"@image"}},
			wantPrompt: "a robot playing chess",
		},
		{
			name:       "empty trigger phrase in config",
			text:       "@image sunset",
			cfg:        &Config{Enabled: true, TriggerPhrases: []string{"", "@image"}},
			wantPrompt: "sunset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := client.DetectImageRequest(tt.text, tt.cfg)
			if tt.wantNil {
				if req != nil {
					t.Errorf("expected nil request, got %+v", req)
				}
				return
			}
			if req == nil {
				t.Fatal("expected non-nil request")
			}
			if req.Prompt != tt.wantPrompt {
				t.Errorf("Prompt = %q, want %q", req.Prompt, tt.wantPrompt)
			}
			if req.Config != tt.cfg {
				t.Error("Config not preserved in request")
			}
		})
	}
}

func TestTruncateForAlt(t *testing.T) {
	tests := []struct {
		input  string
		max    int
		output string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"longer than ten", 10, "longer ..."},
		{"a very long prompt that should be truncated for alt text", 20, "a very long promp..."},
		{"", 10, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateForAlt(tt.input, tt.max)
			if got != tt.output {
				t.Errorf("truncateForAlt(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.output)
			}
		})
	}
}

func TestConfigIsEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"nil config", nil, false},
		{"disabled", &Config{Enabled: false}, false},
		{"enabled", &Config{Enabled: true}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigGetProvider(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{"nil config", nil, "gemini"},
		{"empty provider", &Config{Provider: ""}, "gemini"},
		{"gemini", &Config{Provider: "gemini"}, "gemini"},
		{"openai", &Config{Provider: "openai"}, "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetProvider(); got != tt.want {
				t.Errorf("GetProvider() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigGetModel(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{"nil config", nil, ""},
		{"empty model", &Config{Model: ""}, ""},
		{"custom model", &Config{Model: "dall-e-3"}, "dall-e-3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetModel(); got != tt.want {
				t.Errorf("GetModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateValidationAndProviderSelection(t *testing.T) {
	client := NewClient()
	if _, err := client.Generate(context.Background(), nil); err == nil {
		t.Fatal("nil request should fail")
	}
	if _, err := client.Generate(context.Background(), &ImageRequest{Config: &Config{Provider: "unknown"}}); err == nil {
		t.Fatal("unsupported provider should fail")
	}
	if _, err := client.Generate(context.Background(), &ImageRequest{Config: &Config{Provider: "gemini"}}); err == nil {
		t.Fatal("gemini without a key should fail before network access")
	}
	if _, err := client.Generate(context.Background(), &ImageRequest{Config: &Config{Provider: "openai"}}); err == nil {
		t.Fatal("openai without a key should fail before network access")
	}
}

func TestImageEncodingHelpers(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	jpegData, width, height := convertToJPEG(pngData.Bytes())
	if len(jpegData) == 0 || width != 2 || height != 3 {
		t.Fatalf("convertToJPEG dimensions = %d x %d", width, height)
	}
	if width, height := getImageDimensions(pngData.Bytes()); width != 2 || height != 3 {
		t.Fatalf("getImageDimensions = %d x %d", width, height)
	}
	if got, width, height := convertToJPEG([]byte("not-an-image")); string(got) != "not-an-image" || width != 0 || height != 0 {
		t.Fatal("invalid image should be returned unchanged")
	}
}

func TestGenerateGeminiWithDeterministicServer(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("unexpected request")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"inlineData": map[string]string{"mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(pngData.Bytes())}}}}}}})
	}))
	defer server.Close()
	old := geminiImageEndpoint
	geminiImageEndpoint = server.URL + "/%s"
	defer func() { geminiImageEndpoint = old }()
	got, err := NewClient().Generate(context.Background(), &ImageRequest{Prompt: "cat", Config: &Config{Provider: "gemini"}, GeminiAPIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if got.MIMEType != "image/jpeg" || got.Width != 1 || got.Height != 1 {
		t.Fatalf("unexpected generated image: %+v", got)
	}
}
