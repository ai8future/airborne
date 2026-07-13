package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // Register PNG decoder
	"io"
	"net/http"
	"time"

	"github.com/ai8future/airborne/internal/provider"
)

var geminiImageEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"

const (
	defaultGeminiModel = "gemini-2.5-flash-image"
	geminiTimeout      = 90 * time.Second
	jpegQuality        = 85
	maxResponseSize    = 50 * 1024 * 1024 // 50MB
)

type geminiRequest struct {
	Contents         []geminiContent  `json:"contents"`
	GenerationConfig *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiGenConfig struct {
	ResponseModalities []string `json:"responseModalities,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content struct {
		Parts []geminiResponsePart `json:"parts"`
	} `json:"content"`
}

type geminiResponsePart struct {
	Text       string           `json:"text,omitempty"`
	InlineData *geminiImageData `json:"inlineData,omitempty"`
}

type geminiImageData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"` // base64
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func (c *Client) generateGemini(ctx context.Context, req *ImageRequest) (provider.GeneratedImage, error) {
	if req.GeminiAPIKey == "" {
		return provider.GeneratedImage{}, fmt.Errorf("gemini API key not configured for image generation")
	}

	model := req.Config.GetModel()
	if model == "" {
		model = defaultGeminiModel
	}

	// Build request with TEXT + IMAGE modalities
	body := geminiRequest{
		Contents: []geminiContent{{
			Parts: []geminiPart{{Text: req.Prompt}},
		}},
		GenerationConfig: &geminiGenConfig{
			ResponseModalities: []string{"TEXT", "IMAGE"},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return provider.GeneratedImage{}, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf(geminiImageEndpoint, model)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return provider.GeneratedImage{}, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", req.GeminiAPIKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return provider.GeneratedImage{}, fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	// Limit response size
	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return provider.GeneratedImage{}, fmt.Errorf("read response: %w", err)
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return provider.GeneratedImage{}, fmt.Errorf("decode response: %w", err)
	}

	if geminiResp.Error != nil {
		return provider.GeneratedImage{}, fmt.Errorf("gemini error [%d]: %s", geminiResp.Error.Code, geminiResp.Error.Message)
	}

	// Find image in response parts
	for _, candidate := range geminiResp.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				imgData, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					continue // Try next part
				}

				// Convert to JPEG for smaller size
				jpegData, width, height := convertToJPEG(imgData)

				return provider.GeneratedImage{
					Data:     jpegData,
					MIMEType: "image/jpeg",
					Prompt:   req.Prompt,
					AltText:  truncateForAlt(req.Prompt, 125),
					Width:    width,
					Height:   height,
				}, nil
			}
		}
	}

	return provider.GeneratedImage{}, fmt.Errorf("no image found in gemini response")
}

// convertToJPEG converts image data to JPEG format and returns dimensions.
func convertToJPEG(data []byte) ([]byte, int, int) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Return original data if decode fails
		return data, 0, 0
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		// Return original data if encode fails
		return data, 0, 0
	}

	bounds := img.Bounds()
	return buf.Bytes(), bounds.Dx(), bounds.Dy()
}
