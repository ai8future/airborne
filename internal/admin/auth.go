package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotSetup        = errors.New("admin password not set up")
	ErrInvalidPassword = errors.New("invalid password")
	ErrInvalidSession  = errors.New("invalid or expired session")
)

// AdminCredentials represents stored admin credentials
type AdminCredentials struct {
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

// AdminAuth manages admin authentication
type AdminAuth struct {
	credentialsPath string
	credentials     *AdminCredentials
	sessions        map[string]time.Time // token -> expiry
	sessionTTL      time.Duration
	mu              sync.RWMutex
}

// NewAdminAuth creates a new admin auth manager
func NewAdminAuth(configDir string) *AdminAuth {
	return &AdminAuth{
		credentialsPath: filepath.Join(configDir, ".admin_credentials"),
		sessions:        make(map[string]time.Time),
		sessionTTL:      24 * time.Hour,
	}
}

// Load loads credentials from file
func (a *AdminAuth) Load() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := os.ReadFile(a.credentialsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No credentials file - needs setup
			return nil
		}
		return err
	}

	var creds AdminCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return err
	}

	a.credentials = &creds
	return nil
}

// NeedsSetup returns true if admin password hasn't been set
func (a *AdminAuth) NeedsSetup() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.credentials == nil
}

// SetPassword sets the admin password (first-time setup)
func (a *AdminAuth) SetPassword(password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	creds := AdminCredentials{
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(a.credentialsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Write file with restricted permissions
	if err := os.WriteFile(a.credentialsPath, data, 0600); err != nil {
		return err
	}

	a.mu.Lock()
	a.credentials = &creds
	a.mu.Unlock()

	return nil
}

// ValidatePassword validates the admin password and returns a session token
func (a *AdminAuth) ValidatePassword(password string) (string, error) {
	a.mu.RLock()
	creds := a.credentials
	a.mu.RUnlock()

	if creds == nil {
		return "", ErrNotSetup
	}

	if err := bcrypt.CompareHashAndPassword([]byte(creds.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidPassword
	}

	// Generate session token
	token, err := generateToken()
	if err != nil {
		return "", err
	}

	a.mu.Lock()
	a.sessions[token] = time.Now().Add(a.sessionTTL)
	a.mu.Unlock()

	return token, nil
}

// ValidateSession validates a session token
func (a *AdminAuth) ValidateSession(token string) bool {
	if token == "" {
		return false
	}

	a.mu.RLock()
	expiry, exists := a.sessions[token]
	a.mu.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		// Expired - clean up
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
		return false
	}

	return true
}

// InvalidateSession removes a session token
func (a *AdminAuth) InvalidateSession(token string) {
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
}

// CleanupExpiredSessions removes expired sessions
func (a *AdminAuth) CleanupExpiredSessions() {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	for token, expiry := range a.sessions {
		if now.After(expiry) {
			delete(a.sessions, token)
		}
	}
}

// StartCleanupRoutine starts a background goroutine to clean up expired sessions
func (a *AdminAuth) StartCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			a.CleanupExpiredSessions()
		}
	}()
}

func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
