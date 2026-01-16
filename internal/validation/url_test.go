package validation

import (
	"errors"
	"net"
	"testing"
)

func TestValidateProviderURL(t *testing.T) {
	originalLookup := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}
	t.Cleanup(func() {
		lookupIP = originalLookup
	})

	tests := []struct {
		name    string
		url     string
		wantErr error
	}{
		// Valid HTTPS URLs
		{
			name:    "valid https URL",
			url:     "https://api.openai.com/v1",
			wantErr: nil,
		},
		{
			name:    "valid https URL with port",
			url:     "https://api.example.com:8443/v1",
			wantErr: nil,
		},
		{
			name:    "valid https URL with path",
			url:     "https://api.anthropic.com/v1/messages",
			wantErr: nil,
		},
		{
			name:    "valid https google URL",
			url:     "https://generativelanguage.googleapis.com",
			wantErr: nil,
		},

		// Localhost HTTP allowed
		{
			name:    "localhost http allowed",
			url:     "http://localhost:8080/v1",
			wantErr: nil,
		},
		{
			name:    "127.0.0.1 http allowed",
			url:     "http://127.0.0.1:8080/v1",
			wantErr: nil,
		},
		{
			name:    "localhost https allowed",
			url:     "https://localhost:8443/v1",
			wantErr: nil,
		},

		// External HTTP blocked
		{
			name:    "external http blocked",
			url:     "http://api.openai.com/v1",
			wantErr: ErrHTTPNotAllowed,
		},
		{
			name:    "external http with IP blocked",
			url:     "http://8.8.8.8:8080/v1",
			wantErr: ErrHTTPNotAllowed,
		},

		// Private IPs blocked
		{
			name:    "private IP 10.x blocked",
			url:     "https://10.0.0.1:8080/v1",
			wantErr: ErrPrivateIP,
		},
		{
			name:    "private IP 172.16.x blocked",
			url:     "https://172.16.0.1:8080/v1",
			wantErr: ErrPrivateIP,
		},
		{
			name:    "private IP 172.31.x blocked",
			url:     "https://172.31.255.255:8080/v1",
			wantErr: ErrPrivateIP,
		},
		{
			name:    "private IP 192.168.x blocked",
			url:     "https://192.168.1.1:8080/v1",
			wantErr: ErrPrivateIP,
		},

		// Metadata endpoint blocked
		{
			name:    "AWS metadata endpoint blocked",
			url:     "https://169.254.169.254/latest/meta-data/",
			wantErr: ErrMetadataEndpoint,
		},
		{
			name:    "metadata http blocked (fails http check first)",
			url:     "http://169.254.169.254/latest/meta-data/",
			wantErr: ErrHTTPNotAllowed, // HTTP check happens before metadata check
		},
		{
			name:    "link-local address blocked",
			url:     "https://169.254.1.1/test",
			wantErr: ErrMetadataEndpoint,
		},

		// Dangerous protocols blocked
		{
			name:    "file protocol blocked",
			url:     "file:///etc/passwd",
			wantErr: ErrUnsafeProtocol,
		},
		{
			name:    "gopher protocol blocked",
			url:     "gopher://evil.com:70/1",
			wantErr: ErrUnsafeProtocol,
		},
		{
			name:    "javascript protocol blocked",
			url:     "javascript:alert(1)",
			wantErr: ErrUnsafeProtocol,
		},
		{
			name:    "data protocol blocked",
			url:     "data:text/html,<script>alert(1)</script>",
			wantErr: ErrUnsafeProtocol,
		},
		{
			name:    "ftp protocol blocked",
			url:     "ftp://example.com/file",
			wantErr: ErrUnsafeProtocol,
		},
		{
			name:    "ldap protocol blocked",
			url:     "ldap://evil.com/dc=example,dc=com",
			wantErr: ErrUnsafeProtocol,
		},

		// Empty and invalid URLs
		{
			name:    "empty URL blocked",
			url:     "",
			wantErr: ErrEmptyURL,
		},
		{
			name:    "whitespace only blocked",
			url:     "   ",
			wantErr: ErrEmptyURL,
		},
		{
			name:    "missing hostname blocked",
			url:     "https:///path",
			wantErr: ErrInvalidURL,
		},
		{
			name:    "no scheme blocked",
			url:     "api.openai.com/v1",
			wantErr: ErrUnsafeProtocol,
		},

		// Edge cases
		{
			name:    "uppercase scheme normalized",
			url:     "HTTPS://api.openai.com/v1",
			wantErr: nil,
		},
		{
			name:    "mixed case scheme normalized",
			url:     "HtTpS://api.openai.com/v1",
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProviderURL(tt.url)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateProviderURL(%q) = %v, want nil", tt.url, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateProviderURL(%q) = nil, want error containing %v", tt.url, tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateProviderURL(%q) = %v, want error containing %v", tt.url, err, tt.wantErr)
				}
			}
		})
	}
}

func TestIsLocalhostHost(t *testing.T) {
	tests := []struct {
		hostname string
		want     bool
	}{
		{"localhost", true},
		{"LOCALHOST", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"192.168.1.1", false},
		{"api.openai.com", false},
		{"10.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			got := isLocalhostHost(tt.hostname)
			if got != tt.want {
				t.Errorf("isLocalhostHost(%q) = %v, want %v", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestIsMetadataEndpoint(t *testing.T) {
	tests := []struct {
		hostname string
		want     bool
	}{
		{"169.254.169.254", true},
		{"169.254.0.1", true},
		{"169.254.255.255", true},
		{"metadata.google.internal", true},
		{"METADATA.GOOGLE.INTERNAL", true},
		{"10.0.0.1", false},
		{"api.openai.com", false},
		{"127.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			got := isMetadataEndpoint(tt.hostname)
			if got != tt.want {
				t.Errorf("isMetadataEndpoint(%q) = %v, want %v", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		// Private ranges
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.255.255", true},

		// Non-private
		{"172.15.0.1", false},  // Just outside 172.16-31
		{"172.32.0.1", false},  // Just outside 172.16-31
		{"8.8.8.8", false},     // Public DNS
		{"1.1.1.1", false},     // Cloudflare DNS

		// Loopback (not blocked, handled separately)
		{"127.0.0.1", false},

		// Link-local blocked
		{"169.254.1.1", true},

		// Zero address blocked
		{"0.0.0.0", true},
		{"0.1.2.3", true},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := parseIPHelper(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.want {
				t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func parseIPHelper(s string) []byte {
	// Simple helper to parse IP for testing
	// Note: Using net.ParseIP would create dependency cycle in test
	// This is a simplified version that works for the test cases
	var ip [4]byte
	var parts [4]int
	n, _ := parseIPv4(s, &parts)
	if n != 4 {
		return nil
	}
	for i := 0; i < 4; i++ {
		ip[i] = byte(parts[i])
	}
	return ip[:]
}

func parseIPv4(s string, parts *[4]int) (int, error) {
	part := 0
	partCount := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '.' {
			if partCount >= 4 {
				return 0, nil
			}
			parts[partCount] = part
			partCount++
			part = 0
		} else if s[i] >= '0' && s[i] <= '9' {
			part = part*10 + int(s[i]-'0')
			if part > 255 {
				return 0, nil
			}
		} else {
			return 0, nil
		}
	}
	return partCount, nil
}

func TestValidateProviderURL_ResolvesPrivateIP(t *testing.T) {
	originalLookup := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	}
	t.Cleanup(func() {
		lookupIP = originalLookup
	})

	err := ValidateProviderURL("https://private.example.test")
	if !errors.Is(err, ErrPrivateIP) {
		t.Fatalf("expected ErrPrivateIP, got %v", err)
	}
}

func TestValidateProviderURL_ResolvesMetadataIP(t *testing.T) {
	originalLookup := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	}
	t.Cleanup(func() {
		lookupIP = originalLookup
	})

	err := ValidateProviderURL("https://metadata.example.test")
	if !errors.Is(err, ErrMetadataEndpoint) {
		t.Fatalf("expected ErrMetadataEndpoint, got %v", err)
	}
}
