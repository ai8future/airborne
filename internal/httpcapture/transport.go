// Package httpcapture provides HTTP transport wrappers for capturing
// raw request and response bodies for debugging purposes.
package httpcapture

import (
	"bytes"
	"io"
	"net/http"
)

// Transport wraps an http.RoundTripper to capture request and response bodies.
// Create a new instance for each request to capture its specific payloads.
type Transport struct {
	// Base is the underlying transport. If nil, http.DefaultTransport is used.
	Base http.RoundTripper

	// RequestBody contains the captured request body after RoundTrip completes.
	RequestBody []byte

	// ResponseBody contains the captured response body after RoundTrip completes.
	ResponseBody []byte
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
	// Capture request body if present
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		t.RequestBody = body
		// Restore the body so the SDK can read it
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	// Make the actual request
	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Capture response body if present
	if resp.Body != nil {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		t.ResponseBody = body
		// Restore the body so the SDK can read it
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}

	return resp, nil
}

// Client returns an *http.Client configured to use this capturing transport.
func (t *Transport) Client() *http.Client {
	return &http.Client{Transport: t}
}
