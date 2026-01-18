// Package markdownsvc provides a client wrapper for the markdown_svc gRPC service.
// When enabled, it handles markdown-to-HTML conversion including Mermaid diagrams,
// LaTeX math, and HTML sanitization.
package markdownsvc

import (
	"context"
	"log/slog"
	"sync"
	"time"

	markdownsvc "github.com/ai8future/markdown_svc/clients/go"
)

var (
	mu      sync.RWMutex
	client  *markdownsvc.Client
	addr    string
	enabled bool
)

// Initialize sets up the markdown_svc client with the given address.
// If addr is empty, the service is disabled and RenderHTML returns an error.
// Should be called once during application startup.
func Initialize(svcAddr string) error {
	mu.Lock()
	defer mu.Unlock()

	addr = svcAddr
	enabled = svcAddr != ""

	if !enabled {
		slog.Info("markdown_svc disabled")
		return nil
	}

	// Create client with reasonable timeout
	var err error
	client, err = markdownsvc.NewClient(addr, markdownsvc.WithTimeout(10*time.Second))
	if err != nil {
		// Log but don't fail startup
		slog.Warn("failed to connect to markdown_svc",
			"addr", addr,
			"error", err)
		enabled = false
		client = nil
		return nil
	}

	slog.Info("markdown_svc enabled", "addr", addr)
	return nil
}

// IsEnabled returns true if markdown_svc is configured and connected.
func IsEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return enabled && client != nil
}

// RenderHTML converts markdown to sanitized HTML using the markdown_svc service.
// The service handles:
//   - Mermaid diagrams
//   - LaTeX math
//   - GitHub Flavored Markdown
//   - HTML sanitization
//
// Returns an error if the service is unavailable.
func RenderHTML(ctx context.Context, markdown string) (string, error) {
	mu.RLock()
	c := client
	mu.RUnlock()

	if c == nil {
		return "", ErrNotEnabled
	}

	// Use 5-second timeout for individual requests
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Call the service with GitHub preset and sanitization
	html, err := c.RenderToHTML(ctx, markdown,
		markdownsvc.WithPreset("github"),
		markdownsvc.WithSanitization("github"),
	)
	if err != nil {
		slog.Warn("markdown_svc RenderToHTML failed",
			"error", err)
		return "", err
	}

	return html, nil
}

// Close shuts down the gRPC connection. Safe to call even if not initialized.
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if client != nil {
		err := client.Close()
		client = nil
		enabled = false
		return err
	}
	return nil
}

// ErrNotEnabled is returned when RenderHTML is called but markdown_svc is not configured.
var ErrNotEnabled = &notEnabledError{}

type notEnabledError struct{}

func (e *notEnabledError) Error() string {
	return "markdown_svc not enabled"
}
