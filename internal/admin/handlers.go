package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cliffpyles/aibox/internal/auth"
)

// API Key handlers

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	if s.keyStore == nil {
		s.jsonError(w, "key store not available", http.StatusServiceUnavailable)
		return
	}

	keys, err := s.keyStore.ListKeys(r.Context())
	if err != nil {
		s.jsonError(w, "failed to list keys: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to response format (redact secrets)
	keyList := make([]map[string]interface{}, 0, len(keys))
	for _, key := range keys {
		keyList = append(keyList, map[string]interface{}{
			"id":          key.KeyID,
			"key_id":      key.KeyID,
			"client_id":   key.ClientID,
			"client_name": key.ClientName,
			"permissions": key.Permissions,
			"rate_limits": map[string]int{
				"rpm": key.RateLimits.RequestsPerMinute,
				"rpd": key.RateLimits.RequestsPerDay,
				"tpm": key.RateLimits.TokensPerMinute,
			},
			"created_at": key.CreatedAt,
			"expires_at": key.ExpiresAt,
			"last_used":  key.LastUsed,
		})
	}

	s.jsonResponse(w, map[string]interface{}{"keys": keyList}, http.StatusOK)
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if s.keyStore == nil {
		s.jsonError(w, "key store not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		ClientName  string   `json:"client_name"`
		Permissions []string `json:"permissions"`
		RPM         int      `json:"rpm"`
		RPD         int      `json:"rpd"`
		TPM         int      `json:"tpm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.ClientName == "" {
		s.jsonError(w, "client_name is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if len(req.Permissions) == 0 {
		req.Permissions = []string{"chat"}
	}
	if req.RPM == 0 {
		req.RPM = s.config.RateLimits.DefaultRPM
	}
	if req.RPD == 0 {
		req.RPD = s.config.RateLimits.DefaultRPD
	}
	if req.TPM == 0 {
		req.TPM = s.config.RateLimits.DefaultTPM
	}

	// Convert permissions to auth.Permission type
	perms := make([]auth.Permission, len(req.Permissions))
	for i, p := range req.Permissions {
		perms[i] = auth.Permission(p)
	}

	key, rawKey, err := s.keyStore.CreateKey(r.Context(), auth.CreateKeyParams{
		ClientName:  req.ClientName,
		Permissions: perms,
		RateLimits: auth.RateLimits{
			RequestsPerMinute: req.RPM,
			RequestsPerDay:    req.RPD,
			TokensPerMinute:   req.TPM,
		},
	})
	if err != nil {
		s.jsonError(w, "failed to create key: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"key":         rawKey, // Full key, shown only once
		"key_id":      key.KeyID,
		"client_id":   key.ClientID,
		"client_name": key.ClientName,
		"permissions": key.Permissions,
	}, http.StatusCreated)
}

func (s *Server) handleGetKey(w http.ResponseWriter, r *http.Request) {
	if s.keyStore == nil {
		s.jsonError(w, "key store not available", http.StatusServiceUnavailable)
		return
	}

	keyID := r.PathValue("id")
	if keyID == "" {
		s.jsonError(w, "key id required", http.StatusBadRequest)
		return
	}

	key, err := s.keyStore.GetKey(r.Context(), keyID)
	if err != nil {
		s.jsonError(w, "key not found", http.StatusNotFound)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"id":          key.KeyID,
		"key_id":      key.KeyID,
		"client_id":   key.ClientID,
		"client_name": key.ClientName,
		"permissions": key.Permissions,
		"rate_limits": map[string]int{
			"rpm": key.RateLimits.RequestsPerMinute,
			"rpd": key.RateLimits.RequestsPerDay,
			"tpm": key.RateLimits.TokensPerMinute,
		},
		"created_at": key.CreatedAt,
		"expires_at": key.ExpiresAt,
		"last_used":  key.LastUsed,
	}, http.StatusOK)
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	if s.keyStore == nil {
		s.jsonError(w, "key store not available", http.StatusServiceUnavailable)
		return
	}

	keyID := r.PathValue("id")
	if keyID == "" {
		s.jsonError(w, "key id required", http.StatusBadRequest)
		return
	}

	if err := s.keyStore.DeleteKey(r.Context(), keyID); err != nil {
		s.jsonError(w, "failed to delete key: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.jsonResponse(w, map[string]bool{"success": true}, http.StatusOK)
}

// Tenant handlers

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if s.tenantMgr == nil {
		s.jsonResponse(w, map[string]interface{}{"tenants": []interface{}{}}, http.StatusOK)
		return
	}

	codes := s.tenantMgr.TenantCodes()
	tenants := make([]map[string]interface{}, 0, len(codes))

	for _, code := range codes {
		cfg, ok := s.tenantMgr.Tenant(code)
		if !ok {
			continue
		}
		tenants = append(tenants, map[string]interface{}{
			"code":        cfg.TenantID,
			"name":        cfg.DisplayName,
			"failover":    cfg.Failover,
			"rate_limits": cfg.RateLimits,
			"providers":   cfg.Providers,
		})
	}

	s.jsonResponse(w, map[string]interface{}{"tenants": tenants}, http.StatusOK)
}

func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	if s.tenantMgr == nil {
		s.jsonError(w, "tenant manager not available", http.StatusServiceUnavailable)
		return
	}

	code := r.PathValue("code")
	if code == "" {
		s.jsonError(w, "tenant code required", http.StatusBadRequest)
		return
	}

	cfg, ok := s.tenantMgr.Tenant(code)
	if !ok {
		s.jsonError(w, "tenant not found", http.StatusNotFound)
		return
	}

	s.jsonResponse(w, map[string]interface{}{
		"code":        cfg.TenantID,
		"name":        cfg.DisplayName,
		"failover":    cfg.Failover,
		"rate_limits": cfg.RateLimits,
		"providers":   cfg.Providers,
	}, http.StatusOK)
}

// Usage handlers

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement usage tracking from Redis
	// For now, return placeholder data
	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "7d"
	}

	// Generate placeholder daily data
	dailyTokens := make([]map[string]interface{}, 0)
	now := time.Now()
	days := 7
	if timeRange == "30d" {
		days = 30
	}

	for i := days - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i)
		dailyTokens = append(dailyTokens, map[string]interface{}{
			"date":   date.Format("2006-01-02"),
			"input":  0,
			"output": 0,
		})
	}

	s.jsonResponse(w, map[string]interface{}{
		"range":          timeRange,
		"total_requests": 0,
		"total_tokens":   0,
		"input_tokens":   0,
		"output_tokens":  0,
		"daily_tokens":   dailyTokens,
		"by_provider":    map[string]interface{}{},
		"by_tenant":      map[string]interface{}{},
	}, http.StatusOK)
}

// Provider handlers

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers := make([]map[string]interface{}, 0)

	for name, cfg := range s.config.Providers {
		status := "disabled"
		if cfg.Enabled {
			status = "configured"
		}

		providers = append(providers, map[string]interface{}{
			"name":          name,
			"status":        status,
			"enabled":       cfg.Enabled,
			"default_model": cfg.DefaultModel,
		})
	}

	s.jsonResponse(w, map[string]interface{}{"providers": providers}, http.StatusOK)
}

func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		s.jsonError(w, "provider name required", http.StatusBadRequest)
		return
	}

	cfg, exists := s.config.Providers[name]
	if !exists {
		s.jsonError(w, "provider not found", http.StatusNotFound)
		return
	}

	status := "disabled"
	if cfg.Enabled {
		status = "configured"
	}

	// Get available models based on provider
	models := getProviderModels(name)

	s.jsonResponse(w, map[string]interface{}{
		"name":           name,
		"status":         status,
		"enabled":        cfg.Enabled,
		"default_model":  cfg.DefaultModel,
		"models":         models,
		"requests_today": 0, // TODO: from Redis
		"tokens_today":   0, // TODO: from Redis
	}, http.StatusOK)
}

func getProviderModels(provider string) []string {
	switch provider {
	case "openai":
		return []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-3.5-turbo"}
	case "anthropic":
		return []string{"claude-sonnet-4-20250514", "claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-haiku-20240307"}
	case "gemini":
		return []string{"gemini-2.0-flash", "gemini-1.5-pro", "gemini-1.5-flash"}
	default:
		return []string{}
	}
}

// Helper to get context with timeout
func contextWithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 10*time.Second)
}
