package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai8future/airborne/internal/validation"
)

// DocboxExtractor extracts text using Docbox's Pandoc API.
type DocboxExtractor struct {
	baseURL string
	client  *http.Client
}

// DocboxConfig configures the Docbox extractor.
type DocboxConfig struct {
	// BaseURL is the Pandoc API base URL (default: http://localhost:41273).
	BaseURL string

	// Timeout is the HTTP request timeout (default: 120s for large files).
	Timeout time.Duration
}

// NewDocboxExtractor creates a new Docbox extractor.
func NewDocboxExtractor(cfg DocboxConfig) *DocboxExtractor {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:41273"
	}
	// Validate URL to prevent SSRF
	if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
		slog.Warn("invalid docbox URL, defaulting to safe localhost", "url", cfg.BaseURL, "error", err)
		cfg.BaseURL = "http://localhost:41273"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}

	return &DocboxExtractor{
		baseURL: cfg.BaseURL,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// supportedFormats maps file extensions to Pandoc input formats.
var supportedFormats = map[string]string{
	".pdf":  "pdf",
	".docx": "docx",
	".doc":  "docx",
	".odt":  "odt",
	".rtf":  "rtf",
	".html": "html",
	".htm":  "html",
	".md":   "markdown",
	".txt":  "plain",
	".csv":  "csv",
	".json": "json",
	".epub": "epub",
	".tex":  "latex",
	".rst":  "rst",
}

// SupportedFormats returns the file extensions this extractor can handle.
func (e *DocboxExtractor) SupportedFormats() []string {
	formats := make([]string, 0, len(supportedFormats))
	for ext := range supportedFormats {
		formats = append(formats, ext)
	}
	return formats
}

// Extract reads a document and returns its text content.
func (e *DocboxExtractor) Extract(ctx context.Context, file io.Reader, filename string, mimeType string) (*ExtractionResult, error) {
	ext := strings.ToLower(filepath.Ext(filename))

	// Check if format is supported
	inputFormat, supported := supportedFormats[ext]
	if !supported {
		// For unsupported formats, try to read as plain text
		content, err := io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
		return &ExtractionResult{
			Text:      string(content),
			PageCount: 1,
			Metadata:  map[string]any{"format": "unknown", "fallback": true},
		}, nil
	}

	// For plain text and markdown, read directly
	if inputFormat == "plain" || inputFormat == "markdown" {
		content, err := io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("read file: %w", err)
		}
		return &ExtractionResult{
			Text:      string(content),
			PageCount: 1,
			Metadata:  map[string]any{"format": inputFormat},
		}, nil
	}

	// For other formats, use Pandoc API
	return e.extractViaPandoc(ctx, file, filename, inputFormat)
}

// extractViaPandoc calls the Docbox Pandoc API to convert the file to plain text.
func (e *DocboxExtractor) extractViaPandoc(ctx context.Context, file io.Reader, filename string, inputFormat string) (*ExtractionResult, error) {
	// Read file content
	fileContent, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileContent); err != nil {
		return nil, fmt.Errorf("write file content: %w", err)
	}

	// Add conversion parameters
	if err := writer.WriteField("from", inputFormat); err != nil {
		return nil, fmt.Errorf("write from field: %w", err)
	}
	if err := writer.WriteField("to", "plain"); err != nil {
		return nil, fmt.Errorf("write to field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/convert", &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Try to read error message
		var errorResp struct {
			Detail string `json:"detail"`
		}
		if json.NewDecoder(resp.Body).Decode(&errorResp) == nil && errorResp.Detail != "" {
			return nil, fmt.Errorf("pandoc error: %s", errorResp.Detail)
		}
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pandoc error (status %d): %s", resp.StatusCode, string(body))
	}

	// Read extracted text
	text, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Clean up the text
	cleanText := strings.TrimSpace(string(text))

	// Estimate page count (rough: ~3000 chars per page)
	pageCount := len(cleanText) / 3000
	if pageCount == 0 && len(cleanText) > 0 {
		pageCount = 1
	}

	return &ExtractionResult{
		Text:      cleanText,
		PageCount: pageCount,
		Metadata: map[string]any{
			"format":        inputFormat,
			"original_size": len(fileContent),
		},
	}, nil
}

// IsSupported checks if a file format is supported for extraction.
func (e *DocboxExtractor) IsSupported(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	_, ok := supportedFormats[ext]
	return ok
}
