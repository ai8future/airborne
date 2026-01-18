package tenant

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// dopplerClient fetches secrets from Doppler API.
// It caches secrets per project/config to minimize API calls.
type dopplerClient struct {
	token      string
	config     string // dev, stg, prod
	httpClient *http.Client
	cache      map[string]map[string]string // project -> secret_name -> value
	mu         sync.RWMutex
}

var (
	globalDopplerClient *dopplerClient
	dopplerOnce         sync.Once
)

// initDopplerClient initializes the global Doppler client if DOPPLER_TOKEN is set.
func initDopplerClient() {
	dopplerOnce.Do(func() {
		token := os.Getenv("DOPPLER_TOKEN")
		if token == "" {
			return // Doppler not configured
		}

		config := os.Getenv("DOPPLER_CONFIG")
		if config == "" {
			config = "prod" // Default to production
		}

		globalDopplerClient = &dopplerClient{
			token:  token,
			config: config,
			httpClient: &http.Client{
				Timeout: 10 * time.Second,
			},
			cache: make(map[string]map[string]string),
		}
	})
}

// dopplerSecretResponse represents the API response for fetching secrets.
type dopplerSecretResponse struct {
	Secrets map[string]struct {
		Raw string `json:"raw"`
	} `json:"secrets"`
}

// Retry configuration
const (
	maxRetries  = 15
	baseBackoff = 100 * time.Millisecond
	maxBackoff  = 5 * time.Second
)

// isRetryableError returns true if the error or status code warrants a retry.
func isRetryableError(statusCode int) bool {
	// Retry on server errors (5xx) and rate limiting (429)
	return statusCode >= 500 || statusCode == 429
}

// fetchProjectSecrets fetches all secrets for a project/config from Doppler.
// Uses exponential backoff for transient failures.
func (c *dopplerClient) fetchProjectSecrets(project string) (map[string]string, error) {
	// Check cache first
	c.mu.RLock()
	if secrets, ok := c.cache[project]; ok {
		c.mu.RUnlock()
		return secrets, nil
	}
	c.mu.RUnlock()

	// Fetch with retry
	secrets, err := c.fetchWithRetry(project)
	if err != nil {
		return nil, err
	}

	// Cache the results
	c.mu.Lock()
	c.cache[project] = secrets
	c.mu.Unlock()

	return secrets, nil
}

// fetchWithRetry attempts to fetch secrets with exponential backoff.
func (c *dopplerClient) fetchWithRetry(project string) (map[string]string, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := baseBackoff * time.Duration(1<<(attempt-1)) // doubles each time
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			time.Sleep(backoff)
		}

		secrets, statusCode, err := c.doFetch(project)
		if err == nil {
			return secrets, nil
		}

		lastErr = err

		// Don't retry on client errors (4xx except 429)
		if !isRetryableError(statusCode) && statusCode != 0 {
			return nil, err
		}
	}

	return nil, fmt.Errorf("after %d attempts: %w", maxRetries, lastErr)
}

// doFetch performs a single fetch attempt. Returns secrets, HTTP status code, and error.
func (c *dopplerClient) doFetch(project string) (map[string]string, int, error) {
	url := fmt.Sprintf("https://api.doppler.com/v3/configs/config/secrets?project=%s&config=%s",
		project, c.config)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.SetBasicAuth(c.token, "")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("doppler request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("doppler API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result dopplerSecretResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}

	// Extract raw values
	secrets := make(map[string]string, len(result.Secrets))
	for name, secret := range result.Secrets {
		secrets[name] = secret.Raw
	}

	return secrets, resp.StatusCode, nil
}

// ClearDopplerCache clears the Doppler secret cache.
// Useful for testing or forcing a refresh.
func ClearDopplerCache() {
	if globalDopplerClient != nil {
		globalDopplerClient.mu.Lock()
		globalDopplerClient.cache = make(map[string]map[string]string)
		globalDopplerClient.mu.Unlock()
	}
}

// DopplerEnabled returns true if Doppler is configured (DOPPLER_TOKEN is set).
func DopplerEnabled() bool {
	initDopplerClient()
	return globalDopplerClient != nil
}

// LoadTenantsFromDoppler fetches tenant configs from Doppler.
// It reads BRAND_TENANTS from code_airborne to get the list of brand projects,
// then fetches AIRBORNE_TENANT_CONFIG from each brand project.
func LoadTenantsFromDoppler() (map[string]TenantConfig, error) {
	initDopplerClient()

	if globalDopplerClient == nil {
		return nil, fmt.Errorf("DOPPLER_TOKEN not set")
	}

	// Fetch BRAND_TENANTS from code_airborne
	airborneSecrets, err := globalDopplerClient.fetchProjectSecrets("code_airborne")
	if err != nil {
		return nil, fmt.Errorf("fetch code_airborne secrets: %w", err)
	}

	brandTenantsStr, ok := airborneSecrets["BRAND_TENANTS"]
	if !ok || brandTenantsStr == "" {
		return nil, fmt.Errorf("BRAND_TENANTS not found in code_airborne")
	}

	// Parse comma-separated list of brand projects
	brandProjects := strings.Split(brandTenantsStr, ",")
	result := make(map[string]TenantConfig, len(brandProjects))

	for _, brandProject := range brandProjects {
		brandProject = strings.TrimSpace(brandProject)
		if brandProject == "" {
			continue
		}

		// Fetch AIRBORNE_TENANT_CONFIG from this brand project
		brandSecrets, err := globalDopplerClient.fetchProjectSecrets(brandProject)
		if err != nil {
			return nil, fmt.Errorf("fetch %s secrets: %w", brandProject, err)
		}

		configJSON, ok := brandSecrets["AIRBORNE_TENANT_CONFIG"]
		if !ok || configJSON == "" {
			return nil, fmt.Errorf("AIRBORNE_TENANT_CONFIG not found in %s", brandProject)
		}

		// Parse the JSON config
		var cfg TenantConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("parse AIRBORNE_TENANT_CONFIG from %s: %w", brandProject, err)
		}

		// Normalize tenant ID
		cfg.TenantID = strings.ToLower(strings.TrimSpace(cfg.TenantID))

		// Validate the tenant ID
		if cfg.TenantID == "" {
			return nil, fmt.Errorf("tenant_id missing in AIRBORNE_TENANT_CONFIG from %s", brandProject)
		}

		// Validate tenant config
		if err := validateTenantConfig(&cfg); err != nil {
			return nil, fmt.Errorf("validating tenant from %s: %w", brandProject, err)
		}

		result[cfg.TenantID] = cfg
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no tenant configs loaded from Doppler")
	}

	return result, nil
}
