package httpcapture

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

type mockTransport struct {
	roundTripFunc func(*http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestTransport_RoundTrip(t *testing.T) {
	reqBody := []byte("request payload")
	respBody := []byte("response payload")

	mock := &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			// Verify request body is readable
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(body, reqBody) {
				t.Errorf("expected request body %q, got %q", reqBody, body)
			}
			req.Body.Close()

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}, nil
		},
	}

	tr := New()
	tr.Base = mock

	req, err := http.NewRequest("POST", "http://example.com", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer resp.Body.Close()

	// Verify request captured
	if !bytes.Equal(tr.RequestBody, reqBody) {
		t.Errorf("expected captured request %q, got %q", reqBody, tr.RequestBody)
	}

	// Verify response captured
	if len(tr.ResponseBody) != 0 {
		t.Errorf("response should be captured lazily while response body is read, got %q", tr.ResponseBody)
	}

	// Verify response body is still readable
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if !bytes.Equal(body, respBody) {
		t.Errorf("expected read response %q, got %q", respBody, body)
	}
	if !bytes.Equal(tr.ResponseBody, respBody) {
		t.Errorf("expected captured response %q, got %q", respBody, tr.ResponseBody)
	}
}

func TestTransport_Client(t *testing.T) {
	tr := New()
	client := tr.Client()
	if client.Transport != tr {
		t.Error("client transport mismatch")
	}
}

func TestTransport_TruncatesCapturedBodiesWithoutTruncatingNetworkPayload(t *testing.T) {
	reqBody := bytes.Repeat([]byte("a"), MaxCaptureBodyBytes+128)
	respBody := bytes.Repeat([]byte("b"), MaxCaptureBodyBytes+256)

	mock := &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(body, reqBody) {
				t.Fatalf("network request body was truncated: got %d bytes, want %d", len(body), len(reqBody))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}, nil
		},
	}

	tr := New()
	tr.Base = mock

	req, err := http.NewRequest("POST", "http://example.com?api_key=secret", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	_ = resp.Body.Close()

	if !bytes.Equal(body, respBody) {
		t.Fatalf("network response body was truncated: got %d bytes, want %d", len(body), len(respBody))
	}
	if len(tr.RequestBody) != MaxCaptureBodyBytes || !tr.RequestTruncated {
		t.Fatalf("request capture len=%d truncated=%v, want len=%d truncated=true", len(tr.RequestBody), tr.RequestTruncated, MaxCaptureBodyBytes)
	}
	if len(tr.ResponseBody) != MaxCaptureBodyBytes || !tr.ResponseTruncated {
		t.Fatalf("response capture len=%d truncated=%v, want len=%d truncated=true", len(tr.ResponseBody), tr.ResponseTruncated, MaxCaptureBodyBytes)
	}
}

func TestRedactedURL(t *testing.T) {
	req, err := http.NewRequest("GET", "https://user:pass@example.com/path?key=secret&token=secret&safe=value", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	got := redactedURL(req.URL)
	if strings.Contains(got, "secret") || strings.Contains(got, "user:pass") {
		t.Fatalf("redactedURL leaked sensitive value: %s", got)
	}
	if !strings.Contains(got, "safe=value") {
		t.Fatalf("redactedURL removed non-sensitive query value: %s", got)
	}
}
