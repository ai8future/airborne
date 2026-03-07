package imagegen

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/chassis-go/v6/call"
)

// Client handles image generation via external providers.
type Client struct {
	httpClient *call.Client
}

// NewClient creates a new image generation client.
func NewClient() *Client {
	return &Client{
		httpClient: call.New(
			call.WithTimeout(90*time.Second),
			call.WithRetry(2, 1*time.Second),
			call.WithCircuitBreaker("gemini-imagegen", 3, 60*time.Second),
		),
	}
}

// ImageRequest represents a detected image generation request.
type ImageRequest struct {
	// Prompt is the text description for image generation
	Prompt string

	// Config contains the image generation settings
	Config *Config

	// GeminiAPIKey is the API key for Gemini image generation
	GeminiAPIKey string

	// OpenAIAPIKey is the API key for OpenAI/DALL-E image generation
	OpenAIAPIKey string
}

// DetectImageRequest checks text against configured trigger phrases.
// Returns nil if no trigger found or image generation is disabled.
func (c *Client) DetectImageRequest(text string, cfg *Config) *ImageRequest {
	if cfg == nil || !cfg.IsEnabled() {
		return nil
	}

	if len(cfg.TriggerPhrases) == 0 {
		return nil
	}

	lowerText := strings.ToLower(text)
	for _, trigger := range cfg.TriggerPhrases {
		lowerTrigger := strings.ToLower(strings.TrimSpace(trigger))
		if lowerTrigger == "" {
			continue
		}

		idx := strings.Index(lowerText, lowerTrigger)
		if idx != -1 {
			// Extract prompt: everything after the trigger phrase
			promptStart := idx + len(lowerTrigger)
			prompt := strings.TrimSpace(text[promptStart:])
			if prompt == "" {
				continue // Need actual prompt content
			}
			return &ImageRequest{
				Prompt: prompt,
				Config: cfg,
			}
		}
	}
	return nil
}

// Generate creates an image using the configured provider.
func (c *Client) Generate(ctx context.Context, req *ImageRequest) (provider.GeneratedImage, error) {
	if req == nil || req.Config == nil {
		return provider.GeneratedImage{}, fmt.Errorf("invalid request: nil request or config")
	}

	prov := req.Config.GetProvider()

	switch prov {
	case "gemini":
		return c.generateGemini(ctx, req)
	case "openai":
		return c.generateOpenAI(ctx, req)
	default:
		return provider.GeneratedImage{}, fmt.Errorf("unsupported image provider: %s", prov)
	}
}

// truncateForAlt truncates a string for use as alt text.
func truncateForAlt(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
