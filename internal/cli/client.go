package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

type Client struct {
	BaseURL    string
	AuthToken  string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:   baseURL,
		AuthToken: os.Getenv("AIRBORNE_ADMIN_TOKEN"),
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) setAuth(req *http.Request) {
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	c.setAuth(req)
	return c.HTTPClient.Do(req)
}

func readErrorBody(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 64*1024+1))
	if len(data) > 64*1024 {
		return string(data[:64*1024]) + "...(truncated)"
	}
	return string(data)
}

// Response types

type HealthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

type Activity struct {
	ID               string  `json:"id"`
	ThreadID         string  `json:"thread_id"`
	Tenant           string  `json:"tenant"`
	Content          string  `json:"content"`
	FullContent      string  `json:"full_content"`
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TokensUsed       int     `json:"tokens_used"`
	CostUSD          float64 `json:"cost_usd"`
	GroundingCostUSD float64 `json:"grounding_cost_usd"`
	GroundingQueries int     `json:"grounding_queries"`
	ProcessingTimeMs int     `json:"processing_time_ms"`
	Status           string  `json:"status"`
	Timestamp        string  `json:"timestamp"`
	UserID           string  `json:"user_id"`
}

type ActivityResponse struct {
	Activity []Activity `json:"activity"`
}

type TestRequest struct {
	Prompt   string `json:"prompt"`
	TenantID string `json:"tenant_id"`
	Provider string `json:"provider,omitempty"`
}

type TestResponse struct {
	Error        string `json:"error,omitempty"`
	Reply        string `json:"reply"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	ProcessingMs int    `json:"processing_ms"`
}

type DebugResponse struct {
	MessageID        string  `json:"message_id"`
	ThreadID         string  `json:"thread_id"`
	TenantID         string  `json:"tenant_id"`
	UserID           string  `json:"user_id"`
	Timestamp        string  `json:"timestamp"`
	SystemPrompt     string  `json:"system_prompt"`
	UserInput        string  `json:"user_input"`
	RequestModel     string  `json:"request_model"`
	RequestProvider  string  `json:"request_provider"`
	ResponseText     string  `json:"response_text"`
	ResponseModel    string  `json:"response_model"`
	TokensIn         int     `json:"tokens_in"`
	TokensOut        int     `json:"tokens_out"`
	CostUSD          float64 `json:"cost_usd"`
	GroundingQueries int     `json:"grounding_queries"`
	GroundingCostUSD float64 `json:"grounding_cost_usd"`
	DurationMs       int     `json:"duration_ms"`
	RawRequestJSON   string  `json:"raw_request_json"`
	RawResponseJSON  string  `json:"raw_response_json"`
	Status           string  `json:"status"`
}

type ThreadMessage struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Model     string `json:"model,omitempty"`
	Timestamp string `json:"timestamp"`
}

type ThreadResponse struct {
	ThreadID string          `json:"thread_id"`
	Messages []ThreadMessage `json:"messages"`
}

// API methods

func (c *Client) Health() (*HealthResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/admin/health", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check failed (HTTP %d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &health, nil
}

func (c *Client) Activity(limit int, tenantID string) (*ActivityResponse, error) {
	u, err := url.Parse(c.BaseURL + "/admin/activity")
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("limit", fmt.Sprintf("%d", limit))
	if tenantID != "" {
		q.Set("tenant_id", tenantID)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed (HTTP %d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var activity ActivityResponse
	if err := json.NewDecoder(resp.Body).Decode(&activity); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &activity, nil
}

func (c *Client) Test(req TestRequest) (*TestResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, c.BaseURL+"/admin/test", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed (HTTP %d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var testResp TestResponse
	if err := json.NewDecoder(resp.Body).Decode(&testResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if testResp.Error != "" {
		return nil, fmt.Errorf("test request failed: %s", testResp.Error)
	}
	return &testResp, nil
}

func (c *Client) Debug(messageID string) (*DebugResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/admin/debug/"+url.PathEscape(messageID), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed (HTTP %d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var debug DebugResponse
	if err := json.NewDecoder(resp.Body).Decode(&debug); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &debug, nil
}

func (c *Client) Thread(threadID string) (*ThreadResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/admin/thread/"+url.PathEscape(threadID), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed (HTTP %d): %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var thread ThreadResponse
	if err := json.NewDecoder(resp.Body).Decode(&thread); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &thread, nil
}
