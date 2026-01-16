package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ai8future/airborne/internal/redis"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultKeyPrefix = "aibox:key:"

	// API key format constants
	apiKeyPrefix    = "aibox_sk_"
	apiKeyPrefixLen = 9  // len("aibox_sk_")
	keyIDLength     = 8
	minAPIKeyLength = apiKeyPrefixLen + keyIDLength + 2 // prefix + keyid + _ + at least 1 char secret
)

// Permission represents an API key permission
type Permission string

const (
	PermissionChat       Permission = "chat"
	PermissionChatStream Permission = "chat:stream"
	PermissionFiles      Permission = "files"
	PermissionAdmin      Permission = "admin"
)

// RateLimits defines rate limits for a client
type RateLimits struct {
	RequestsPerMinute int `json:"rpm"`
	RequestsPerDay    int `json:"rpd"`
	TokensPerMinute   int `json:"tpm"`
}

// ClientKey represents an API key and its metadata
type ClientKey struct {
	KeyID       string            `json:"key_id"`
	ClientID    string            `json:"client_id"`
	ClientName  string            `json:"client_name"`
	SecretHash  string            `json:"secret_hash"`
	Permissions []Permission      `json:"permissions"`
	RateLimits  RateLimits        `json:"rate_limits"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	LastUsed    *time.Time        `json:"last_used,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// KeyStore manages API keys in Redis
type KeyStore struct {
	redis     *redis.Client
	keyPrefix string
}

// NewKeyStore creates a new key store with default (global) prefix
func NewKeyStore(redis *redis.Client) *KeyStore {
	return &KeyStore{
		redis:     redis,
		keyPrefix: defaultKeyPrefix,
	}
}

// NewTenantKeyStore creates a key store scoped to a specific tenant
func NewTenantKeyStore(redis *redis.Client, tenantID string) *KeyStore {
	prefix := defaultKeyPrefix
	if tenantID != "" {
		prefix = fmt.Sprintf("aibox:%s:key:", tenantID)
	}
	return &KeyStore{
		redis:     redis,
		keyPrefix: prefix,
	}
}

// GenerateAPIKey generates a new API key
// Returns the full key (to give to client) and the ClientKey record
func (s *KeyStore) GenerateAPIKey(ctx context.Context, clientID, clientName string, permissions []Permission, limits RateLimits) (string, *ClientKey, error) {
	// Generate key ID and secret
	keyID, err := generateRandomString(8)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate key ID: %w", err)
	}

	secret, err := generateRandomString(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate secret: %w", err)
	}

	// Hash the secret for storage
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, fmt.Errorf("failed to hash secret: %w", err)
	}

	// Create key record
	key := &ClientKey{
		KeyID:       keyID,
		ClientID:    clientID,
		ClientName:  clientName,
		SecretHash:  string(hash),
		Permissions: permissions,
		RateLimits:  limits,
		CreatedAt:   time.Now().UTC(),
		Metadata:    make(map[string]string),
	}

	// Store in Redis
	if err := s.saveKey(ctx, key); err != nil {
		return "", nil, err
	}

	// Return the full API key (prefix_keyid_secret)
	fullKey := fmt.Sprintf("%s%s_%s", apiKeyPrefix, keyID, secret)
	return fullKey, key, nil
}

// ValidateKey validates an API key and returns the client info
func (s *KeyStore) ValidateKey(ctx context.Context, apiKey string) (*ClientKey, error) {
	// Parse the key
	keyID, secret, err := parseAPIKey(apiKey)
	if err != nil {
		return nil, err
	}

	// Lookup key in Redis
	key, err := s.getKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	// Check expiration
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return nil, ErrKeyExpired
	}

	// Verify secret
	if err := bcrypt.CompareHashAndPassword([]byte(key.SecretHash), []byte(secret)); err != nil {
		return nil, ErrInvalidKey
	}

	return key, nil
}

// GetKey retrieves a key by ID (without validation)
func (s *KeyStore) GetKey(ctx context.Context, keyID string) (*ClientKey, error) {
	return s.getKey(ctx, keyID)
}

// DeleteKey deletes an API key
func (s *KeyStore) DeleteKey(ctx context.Context, keyID string) error {
	return s.redis.Del(ctx, s.keyPrefix+keyID)
}

// ListKeys returns all API keys (without secrets)
func (s *KeyStore) ListKeys(ctx context.Context) ([]*ClientKey, error) {
	// Scan for all keys with our prefix
	pattern := s.keyPrefix + "*"
	keyNames, err := s.redis.Scan(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	keys := make([]*ClientKey, 0, len(keyNames))
	for _, keyName := range keyNames {
		// Extract keyID from full key name
		keyID := keyName[len(s.keyPrefix):]
		key, err := s.getKey(ctx, keyID)
		if err != nil {
			// Skip keys that can't be loaded (may have expired)
			continue
		}
		key.SecretHash = "" // Redact to prevent offline cracking attempts
		keys = append(keys, key)
	}

	return keys, nil
}

// CreateKeyParams holds parameters for creating a new API key
type CreateKeyParams struct {
	ClientName  string
	Permissions []Permission
	RateLimits  RateLimits
}

// CreateKey creates a new API key with auto-generated client ID
// Returns the full key (shown once) and the ClientKey record
func (s *KeyStore) CreateKey(ctx context.Context, params CreateKeyParams) (*ClientKey, string, error) {
	// Generate a unique client ID
	clientID, err := generateRandomString(12)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate client ID: %w", err)
	}

	fullKey, key, err := s.GenerateAPIKey(ctx, clientID, params.ClientName, params.Permissions, params.RateLimits)
	if err != nil {
		return nil, "", err
	}

	return key, fullKey, nil
}

// HasPermission checks if a key has a specific permission
func (k *ClientKey) HasPermission(perm Permission) bool {
	for _, p := range k.Permissions {
		if p == perm || p == PermissionAdmin {
			return true
		}
	}
	return false
}

// saveKey saves a key to Redis
func (s *KeyStore) saveKey(ctx context.Context, key *ClientKey) error {
	data, err := json.Marshal(key)
	if err != nil {
		return fmt.Errorf("failed to marshal key: %w", err)
	}

	var expiration time.Duration
	if key.ExpiresAt != nil {
		expiration = time.Until(*key.ExpiresAt)
		if expiration <= 0 {
			return ErrKeyExpired
		}
	}

	return s.redis.Set(ctx, s.keyPrefix+key.KeyID, string(data), expiration)
}

// getKey retrieves a key from Redis
func (s *KeyStore) getKey(ctx context.Context, keyID string) (*ClientKey, error) {
	data, err := s.redis.Get(ctx, s.keyPrefix+keyID)
	if err != nil {
		if redis.IsNil(err) {
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("failed to get key: %w", err)
	}

	var key ClientKey
	if err := json.Unmarshal([]byte(data), &key); err != nil {
		return nil, fmt.Errorf("data corruption in key store for %q: %w", keyID, err)
	}

	return &key, nil
}

// parseAPIKey parses an API key string into keyID and secret
func parseAPIKey(apiKey string) (keyID, secret string, err error) {
	// Expected format: aibox_sk_KEYID_SECRET
	if len(apiKey) < minAPIKeyLength {
		return "", "", ErrInvalidKey
	}

	// Check prefix
	if apiKey[:apiKeyPrefixLen] != apiKeyPrefix {
		return "", "", ErrInvalidKey
	}

	// Extract keyID (8 chars) and secret (rest)
	remainder := apiKey[apiKeyPrefixLen:]
	if len(remainder) < keyIDLength+2 { // keyID + _ + at least 1 char secret
		return "", "", ErrInvalidKey
	}

	keyID = remainder[:keyIDLength]
	if remainder[keyIDLength] != '_' {
		return "", "", ErrInvalidKey
	}
	secret = remainder[keyIDLength+1:]

	return keyID, secret, nil
}

// generateRandomString generates a random hex string
func generateRandomString(length int) (string, error) {
	// We need length/2 bytes to produce 'length' hex characters
	// Round up to handle odd lengths
	byteLen := (length + 1) / 2
	bytes := make([]byte, byteLen)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Hex encode and truncate to exact requested length
	return hex.EncodeToString(bytes)[:length], nil
}
