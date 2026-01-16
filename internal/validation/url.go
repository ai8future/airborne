package validation

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var (
	// ErrEmptyURL is returned when the URL is empty
	ErrEmptyURL = errors.New("URL cannot be empty")

	// ErrInvalidURL is returned when the URL cannot be parsed
	ErrInvalidURL = errors.New("invalid URL format")

	// ErrUnsafeProtocol is returned when the URL uses a dangerous protocol
	ErrUnsafeProtocol = errors.New("unsafe protocol")

	// ErrHTTPNotAllowed is returned when HTTP is used for non-localhost URLs
	ErrHTTPNotAllowed = errors.New("HTTP is only allowed for localhost")

	// ErrPrivateIP is returned when the URL resolves to a private IP
	ErrPrivateIP = errors.New("private/internal IP addresses are not allowed")

	// ErrMetadataEndpoint is returned when the URL targets a cloud metadata endpoint
	ErrMetadataEndpoint = errors.New("cloud metadata endpoints are not allowed")
)

// dangerousProtocols contains protocols that should never be allowed
var dangerousProtocols = map[string]bool{
	"file":       true,
	"gopher":     true,
	"javascript": true,
	"data":       true,
	"ftp":        true,
	"dict":       true,
	"ldap":       true,
	"ldaps":      true,
	"tftp":       true,
}

// lookupIP allows tests to stub DNS resolution.
var lookupIP = net.LookupIP

// validateHostnameResolvesPublic checks that a hostname doesn't resolve to private/metadata IPs.
func validateHostnameResolvesPublic(hostname string) error {
	ips, err := lookupIP(hostname)
	if err != nil {
		return fmt.Errorf("%w: DNS lookup failed for %s: %v", ErrInvalidURL, hostname, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: DNS lookup returned no results for %s", ErrInvalidURL, hostname)
	}

	for _, ip := range ips {
		if ip == nil {
			continue
		}
		if isMetadataEndpoint(ip.String()) {
			return fmt.Errorf("%w: %s resolves to metadata IP %s", ErrMetadataEndpoint, hostname, ip.String())
		}
		if ip.IsLoopback() || isPrivateIP(ip) {
			return fmt.Errorf("%w: %s resolves to private IP %s", ErrPrivateIP, hostname, ip.String())
		}
	}

	return nil
}

// ValidateProviderURL validates a URL intended for use as a provider base URL.
// It performs SSRF protection by:
// - Rejecting empty URLs
// - Only allowing https:// (or http:// for localhost/127.0.0.1)
// - Blocking dangerous protocols (file://, gopher://, javascript:, data:, etc.)
// - Blocking private/internal IP ranges (10.x, 172.16.x, 192.168.x)
// - Blocking cloud metadata endpoints (169.254.169.254)
func ValidateProviderURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ErrEmptyURL
	}

	// Parse the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	// Normalize the scheme to lowercase
	scheme := strings.ToLower(parsedURL.Scheme)

	// Check for dangerous protocols
	if dangerousProtocols[scheme] {
		return fmt.Errorf("%w: %s:// is not allowed", ErrUnsafeProtocol, scheme)
	}

	// Only allow http or https
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: only http:// and https:// are allowed", ErrUnsafeProtocol)
	}

	// Extract the hostname (without port)
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("%w: missing hostname", ErrInvalidURL)
	}

	// Check if it's localhost
	isLocalhost := isLocalhostHost(hostname)

	// HTTP is only allowed for localhost
	if scheme == "http" && !isLocalhost {
		return fmt.Errorf("%w: use https:// for non-localhost URLs", ErrHTTPNotAllowed)
	}

	// Check for metadata endpoint
	if isMetadataEndpoint(hostname) {
		return fmt.Errorf("%w: %s is blocked", ErrMetadataEndpoint, hostname)
	}

	// Parse IP address if it's a direct IP
	ip := net.ParseIP(hostname)
	if ip != nil {
		// Allow localhost IPs for http://
		if isLocalhost {
			return nil
		}

		// Block private IPs
		if isPrivateIP(ip) {
			return fmt.Errorf("%w: %s is in a private IP range", ErrPrivateIP, hostname)
		}
	}

	// For non-IP hostnames (not localhost), verify they don't resolve to private IPs
	if ip == nil && !isLocalhost {
		if err := validateHostnameResolvesPublic(hostname); err != nil {
			return err
		}
	}

	return nil
}

// isLocalhostHost checks if the hostname is localhost or a loopback address
func isLocalhostHost(hostname string) bool {
	hostname = strings.ToLower(hostname)

	// Check common localhost names
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return true
	}

	// Check if it's a loopback IP
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}

	return false
}

// isMetadataEndpoint checks if the hostname is a cloud metadata endpoint
func isMetadataEndpoint(hostname string) bool {
	// Common cloud metadata IP addresses
	metadataIPs := []string{
		"169.254.169.254", // AWS, GCP, Azure
		"fd00:ec2::254",   // AWS IPv6
		"metadata.google.internal",
		"metadata.gcp.internal",
	}

	hostname = strings.ToLower(hostname)
	for _, meta := range metadataIPs {
		if hostname == meta {
			return true
		}
	}

	// Check for 169.254.x.x range (link-local)
	if ip := net.ParseIP(hostname); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			if ip4[0] == 169 && ip4[1] == 254 {
				return true
			}
		}
	}

	return false
}

// isPrivateIP checks if an IP address is in a private/internal range
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// Check for standard private ranges
	if ip.IsPrivate() {
		return true
	}

	// Check for loopback (handled separately for localhost allowance)
	if ip.IsLoopback() {
		return false // Allow loopback, handled by localhost check
	}

	// Check for link-local
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// Check for unspecified address
	if ip.IsUnspecified() {
		return true
	}

	// Additional check for IPv4 mapped IPv6 addresses
	if ip4 := ip.To4(); ip4 != nil {
		// 0.0.0.0/8 - Current network
		if ip4[0] == 0 {
			return true
		}
	}

	return false
}
