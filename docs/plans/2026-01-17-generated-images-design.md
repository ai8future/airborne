# Generated Images Feature Design

**Date:** 2026-01-17
**Status:** Approved
**Priority:** High

## Overview

Add support for AI-generated images in Airborne responses, matching Solstice's functionality. When an LLM response contains an image trigger (e.g., `@image`), Airborne will generate an image and include it in the gRPC response.

## Goals

1. Support image generation via Gemini (default) and OpenAI DALL-E (fallback)
2. Return generated images as bytes in gRPC responses
3. Use config-driven trigger phrases (from project specs)
4. Maintain parity with Solstice's implementation

## Non-Goals

- StructuredMetadata (separate feature)
- URL-based image delivery
- Multiple images per request
- Subject line trigger detection (response text only)

---

## Type Definitions

### Provider Types (`internal/provider/provider.go`)

```go
// GeneratedImage represents an image produced by an image generation service
type GeneratedImage struct {
    Data      []byte  // Raw image bytes (JPEG)
    MIMEType  string  // "image/jpeg"
    Prompt    string  // The prompt used to generate this image
    AltText   string  // Accessibility alt text
    Width     int     // Image width in pixels
    Height    int     // Image height in pixels
    ContentID string  // Content-ID for email embedding (cid:xxx)
}

// Add to GenerateResult:
type GenerateResult struct {
    // ... existing fields ...
    Images []GeneratedImage // AI-generated images
}

// Helper method
func (r GenerateResult) HasImages() bool {
    return len(r.Images) > 0
}
```

### Proto Messages (`api/proto/airborne/v1/airborne.proto`)

```protobuf
message GeneratedImage {
    bytes data = 1;          // Raw image bytes (JPEG/PNG)
    string mime_type = 2;    // "image/jpeg" or "image/png"
    string prompt = 3;       // The prompt used to generate
    string alt_text = 4;     // Accessibility description
    int32 width = 5;         // Width in pixels
    int32 height = 6;        // Height in pixels
    string content_id = 7;   // For email embedding (cid:xxx)
}

message GenerateReplyResponse {
    // ... existing fields 1-12 ...
    repeated GeneratedImage images = 13;  // NEW
}

message StreamComplete {
    // ... existing fields 1-8 ...
    repeated GeneratedImage images = 9;   // NEW
}
```

---

## Image Generation Package

### File Structure

```
internal/imagegen/
├── client.go       # Client struct, DetectImageRequest, Generate dispatcher
├── gemini.go       # Gemini implementation (default)
├── openai.go       # DALL-E implementation (fallback)
└── config.go       # ImageGenerationConfig struct
```

### Configuration (`internal/imagegen/config.go`)

```go
package imagegen

// ImageGenerationConfig mirrors bizops/solstice config structure
type ImageGenerationConfig struct {
    Enabled         bool     `json:"enabled"`
    Provider        string   `json:"provider"`         // "gemini" (default), "openai"
    Model           string   `json:"model"`            // "gemini-2.5-flash-image", "dall-e-3"
    TriggerPhrases  []string `json:"trigger_phrases"`  // ["@image", "generate image", ...]
    FallbackOnError bool     `json:"fallback_on_error"`
    MaxImages       int      `json:"max_images"`
}

func (c *ImageGenerationConfig) IsEnabled() bool {
    return c != nil && c.Enabled
}
```

### Client (`internal/imagegen/client.go`)

```go
package imagegen

import (
    "context"
    "strings"

    "airborne/internal/provider"
)

type Client struct {
    geminiAPIKey string
    openaiAPIKey string
}

func NewClient(geminiAPIKey, openaiAPIKey string) *Client {
    return &Client{
        geminiAPIKey: geminiAPIKey,
        openaiAPIKey: openaiAPIKey,
    }
}

type ImageRequest struct {
    Prompt string
    Config *ImageGenerationConfig
}

// DetectImageRequest checks text against configured trigger phrases
func (c *Client) DetectImageRequest(text string, cfg *ImageGenerationConfig) *ImageRequest {
    if cfg == nil || !cfg.IsEnabled() {
        return nil
    }

    lowerText := strings.ToLower(text)
    for _, trigger := range cfg.TriggerPhrases {
        lowerTrigger := strings.ToLower(trigger)
        if idx := strings.Index(lowerText, lowerTrigger); idx != -1 {
            promptStart := idx + len(lowerTrigger)
            prompt := strings.TrimSpace(text[promptStart:])
            if prompt == "" {
                continue
            }
            return &ImageRequest{Prompt: prompt, Config: cfg}
        }
    }
    return nil
}

// Generate creates an image using the configured provider
func (c *Client) Generate(ctx context.Context, req *ImageRequest) (provider.GeneratedImage, error) {
    prov := req.Config.Provider
    if prov == "" {
        prov = "gemini" // Default
    }

    switch prov {
    case "gemini":
        return c.generateGemini(ctx, req)
    case "openai":
        return c.generateOpenAI(ctx, req)
    default:
        return provider.GeneratedImage{}, fmt.Errorf("unsupported image provider: %s", prov)
    }
}
```

### Gemini Implementation (`internal/imagegen/gemini.go`)

```go
package imagegen

import (
    "bytes"
    "context"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "image"
    "image/jpeg"
    _ "image/png"
    "net/http"
    "time"

    "airborne/internal/provider"
)

const (
    geminiImageEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"
    defaultGeminiModel  = "gemini-2.5-flash-image"
    geminiTimeout       = 90 * time.Second
    jpegQuality         = 85
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
    Candidates []struct {
        Content struct {
            Parts []struct {
                Text       string `json:"text,omitempty"`
                InlineData *struct {
                    MIMEType string `json:"mimeType"`
                    Data     string `json:"data"`
                } `json:"inlineData,omitempty"`
            } `json:"parts"`
        } `json:"content"`
    } `json:"candidates"`
    Error *struct {
        Code    int    `json:"code"`
        Message string `json:"message"`
    } `json:"error"`
}

func (c *Client) generateGemini(ctx context.Context, req *ImageRequest) (provider.GeneratedImage, error) {
    model := req.Config.Model
    if model == "" {
        model = defaultGeminiModel
    }

    body := geminiRequest{
        Contents: []geminiContent{{
            Parts: []geminiPart{{Text: req.Prompt}},
        }},
        GenerationConfig: &geminiGenConfig{
            ResponseModalities: []string{"TEXT", "IMAGE"},
        },
    }

    jsonBody, _ := json.Marshal(body)
    url := fmt.Sprintf(geminiImageEndpoint, model)

    httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("x-goog-api-key", c.geminiAPIKey)

    client := &http.Client{Timeout: geminiTimeout}
    resp, err := client.Do(httpReq)
    if err != nil {
        return provider.GeneratedImage{}, fmt.Errorf("gemini request: %w", err)
    }
    defer resp.Body.Close()

    var geminiResp geminiResponse
    if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
        return provider.GeneratedImage{}, fmt.Errorf("decode response: %w", err)
    }

    if geminiResp.Error != nil {
        return provider.GeneratedImage{}, fmt.Errorf("gemini error: %s", geminiResp.Error.Message)
    }

    // Find image in response parts
    for _, candidate := range geminiResp.Candidates {
        for _, part := range candidate.Content.Parts {
            if part.InlineData != nil {
                imgData, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
                if err != nil {
                    continue
                }

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

    return provider.GeneratedImage{}, fmt.Errorf("no image in response")
}

func convertToJPEG(pngData []byte) ([]byte, int, int) {
    img, _, err := image.Decode(bytes.NewReader(pngData))
    if err != nil {
        return pngData, 0, 0
    }

    var buf bytes.Buffer
    jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality})

    bounds := img.Bounds()
    return buf.Bytes(), bounds.Dx(), bounds.Dy()
}

func truncateForAlt(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}
```

### OpenAI Implementation (`internal/imagegen/openai.go`)

```go
package imagegen

import (
    "bytes"
    "context"
    "encoding/base64"
    "fmt"
    "image"
    _ "image/png"

    "github.com/openai/openai-go"
    "github.com/openai/openai-go/option"

    "airborne/internal/provider"
)

func (c *Client) generateOpenAI(ctx context.Context, req *ImageRequest) (provider.GeneratedImage, error) {
    client := openai.NewClient(option.WithAPIKey(c.openaiAPIKey))

    model := req.Config.Model
    if model == "" {
        model = "dall-e-3"
    }

    resp, err := client.Images.Generate(ctx, openai.ImageGenerateParams{
        Prompt:         openai.String(req.Prompt),
        Model:          openai.F(openai.ImageModel(model)),
        N:              openai.Int(1),
        Size:           openai.F(openai.ImageGenerateParamsSize1024x1024),
        ResponseFormat: openai.F(openai.ImageGenerateParamsResponseFormatB64JSON),
    })
    if err != nil {
        return provider.GeneratedImage{}, fmt.Errorf("openai image generation: %w", err)
    }

    if len(resp.Data) == 0 {
        return provider.GeneratedImage{}, fmt.Errorf("no image returned")
    }

    imgData, err := base64.StdEncoding.DecodeString(resp.Data[0].B64JSON)
    if err != nil {
        return provider.GeneratedImage{}, fmt.Errorf("decode image: %w", err)
    }

    width, height := getImageDimensions(imgData)

    return provider.GeneratedImage{
        Data:     imgData,
        MIMEType: "image/png",
        Prompt:   req.Prompt,
        AltText:  truncateForAlt(req.Prompt, 125),
        Width:    width,
        Height:   height,
    }, nil
}

func getImageDimensions(data []byte) (int, int) {
    img, _, err := image.DecodeConfig(bytes.NewReader(data))
    if err != nil {
        return 0, 0
    }
    return img.Width, img.Height
}
```

---

## Service Integration

### ChatService Changes (`internal/service/chat.go`)

```go
type ChatService struct {
    // ... existing fields ...
    imageGen *imagegen.Client
}

func (s *ChatService) GenerateReply(ctx context.Context, req *pb.GenerateReplyRequest) (*pb.GenerateReplyResponse, error) {
    // ... existing prepareRequest, provider selection ...

    result, err := selectedProvider.GenerateReply(ctx, params)
    if err != nil {
        return nil, err
    }

    // Check for image generation trigger in response
    var generatedImages []provider.GeneratedImage
    if imgCfg := s.getImageGenConfig(tenantID, projectID); imgCfg.IsEnabled() {
        if imgReq := s.imageGen.DetectImageRequest(result.Text, imgCfg); imgReq != nil {
            img, err := s.imageGen.Generate(ctx, imgReq)
            if err != nil {
                slog.Warn("image generation failed", "error", err)
            } else {
                generatedImages = append(generatedImages, img)
            }
        }
    }

    return &pb.GenerateReplyResponse{
        Text:   result.Text,
        // ... existing fields ...
        Images: convertToProtoImages(generatedImages),
    }, nil
}

func convertToProtoImages(images []provider.GeneratedImage) []*pb.GeneratedImage {
    if len(images) == 0 {
        return nil
    }
    result := make([]*pb.GeneratedImage, len(images))
    for i, img := range images {
        result[i] = &pb.GeneratedImage{
            Data:      img.Data,
            MimeType:  img.MIMEType,
            Prompt:    img.Prompt,
            AltText:   img.AltText,
            Width:     int32(img.Width),
            Height:    int32(img.Height),
            ContentId: img.ContentID,
        }
    }
    return result
}
```

### Streaming Support

For streaming responses, images are included in `StreamComplete`:

```go
case chunk.Type == provider.ChunkTypeComplete:
    var images []provider.GeneratedImage
    if imgCfg := s.getImageGenConfig(tenantID, projectID); imgCfg.IsEnabled() {
        if imgReq := s.imageGen.DetectImageRequest(accumulatedText, imgCfg); imgReq != nil {
            img, _ := s.imageGen.Generate(ctx, imgReq)
            images = append(images, img)
        }
    }

    stream.Send(&pb.GenerateReplyChunk{
        Chunk: &pb.GenerateReplyChunk_Complete{
            Complete: &pb.StreamComplete{
                // ... existing fields ...
                Images: convertToProtoImages(images),
            },
        },
    })
```

---

## Implementation Order

1. **Add types** - `GeneratedImage` in `internal/provider/provider.go`
2. **Update proto** - Add message and fields, regenerate Go code
3. **Create imagegen package** - `config.go`, `client.go`, `gemini.go`, `openai.go`
4. **Integrate into ChatService** - Inject client, call after LLM response
5. **Wire in main.go** - Initialize client with API keys
6. **Add config support** - Accept `ImageGenerationConfig` from tenant/project

---

## Testing Strategy

1. **Unit tests** for trigger detection with various phrases
2. **Unit tests** for JPEG conversion
3. **Integration tests** with mocked Gemini/OpenAI responses
4. **Manual test** with actual image generation

---

## Future Enhancements

- StructuredMetadata support (separate design)
- URL-based image delivery for bandwidth optimization
- Multiple images per request (MaxImages > 1)
- Subject line trigger detection
