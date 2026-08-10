// Package httpcapture provides HTTP transport wrappers for capturing
// raw request and response bodies for debugging purposes.
package httpcapture

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// MaxCaptureBodyBytes bounds per-body debug capture so a hostile or broken
// provider cannot make the debugging transport exhaust memory. The underlying
// HTTP request/response still streams in full; only the retained debug copy is
// capped.
const MaxCaptureBodyBytes = 4 * 1024 * 1024

// Transport wraps an http.RoundTripper to capture request and response bodies.
// Create a new instance for each request to capture its specific payloads.
type Transport struct {
	// Base is the underlying transport. If nil, http.DefaultTransport is used.
	Base http.RoundTripper

	// RequestBody contains the captured request body after RoundTrip completes.
	RequestBody []byte

	// ResponseBody contains the captured response body after RoundTrip completes.
	ResponseBody []byte

	// RequestTruncated/ResponseTruncated indicate that the debug copy hit
	// MaxCaptureBodyBytes. The network payload itself was not truncated.
	RequestTruncated  bool
	ResponseTruncated bool
}

// New creates a new capturing transport with the default base transport.
func New() *Transport {
	return &Transport{
		Base: http.DefaultTransport,
	}
}

// RoundTrip implements http.RoundTripper.
// It captures the request body before sending and the response body after receiving.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.RequestBody = t.RequestBody[:0]
	t.ResponseBody = t.ResponseBody[:0]
	t.RequestTruncated = false
	t.ResponseTruncated = false

	slog.Debug("httpcapture: RoundTrip called",
		"method", req.Method,
		"url", redactedURL(req.URL),
		"has_body", req.Body != nil,
	)

	// Capture request body if present
	if req.Body != nil {
		body, err := io.ReadAll(io.LimitReader(req.Body, MaxCaptureBodyBytes+1))
		if err != nil {
			slog.Warn("httpcapture: failed to read request body", "error", err)
			return nil, err
		}
		if len(body) > MaxCaptureBodyBytes {
			t.RequestBody = append(t.RequestBody[:0], body[:MaxCaptureBodyBytes]...)
			t.RequestTruncated = true
			req.Body = &preserveCloseReadCloser{
				Reader: io.MultiReader(bytes.NewReader(body), req.Body),
				Closer: req.Body,
			}
		} else {
			t.RequestBody = append(t.RequestBody[:0], body...)
			_ = req.Body.Close()
			// Restore the body so the SDK can read it
			req.Body = io.NopCloser(bytes.NewReader(body))
		}

		slog.Debug("httpcapture: captured request body",
			"size", len(t.RequestBody),
			"truncated", t.RequestTruncated,
		)
	}

	// Make the actual request
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		slog.Warn("httpcapture: request failed", "error", err)
		return nil, err
	}

	slog.Debug("httpcapture: response received",
		"status", resp.StatusCode,
		"has_body", resp.Body != nil,
	)

	// Capture response body if present
	if resp.Body != nil {
		resp.Body = &captureReadCloser{
			body:      resp.Body,
			captured:  &t.ResponseBody,
			truncated: &t.ResponseTruncated,
			maxBytes:  MaxCaptureBodyBytes,
		}
	}

	return resp, nil
}

// Client returns an *http.Client configured to use this capturing transport.
func (t *Transport) Client() *http.Client {
	return &http.Client{Transport: t}
}

type preserveCloseReadCloser struct {
	io.Reader
	io.Closer
}

type captureReadCloser struct {
	body      io.ReadCloser
	captured  *[]byte
	truncated *bool
	maxBytes  int
}

func (c *captureReadCloser) Read(p []byte) (int, error) {
	n, err := c.body.Read(p)
	if n > 0 {
		remaining := c.maxBytes - len(*c.captured)
		if remaining > 0 {
			toCapture := n
			if toCapture > remaining {
				toCapture = remaining
				*c.truncated = true
			}
			*c.captured = append(*c.captured, p[:toCapture]...)
		} else {
			*c.truncated = true
		}
	}
	return n, err
}

func (c *captureReadCloser) Close() error {
	slog.Debug("httpcapture: captured response body",
		"size", len(*c.captured),
		"truncated", *c.truncated,
	)
	return c.body.Close()
}

func redactedURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	if clone.User != nil {
		clone.User = url.User("[REDACTED]")
	}
	q := clone.Query()
	changed := false
	for key := range q {
		if sensitiveQueryKey(key) {
			q.Set(key, "[REDACTED]")
			changed = true
		}
	}
	if changed {
		clone.RawQuery = q.Encode()
	}
	return clone.String()
}

func sensitiveQueryKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "key") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "secret") ||
		strings.Contains(k, "password") ||
		strings.Contains(k, "credential")
}
