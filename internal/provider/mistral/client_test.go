package mistral

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "test-key")

	client := NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestNewClientOptionsAndCapabilities(t *testing.T) {
	c := NewClient(nil, WithDebugLogging(true))
	if c.Name() != "mistral" || !c.SupportsStreaming() || c.SupportsWebSearch() || c.SupportsFileSearch() || c.SupportsNativeContinuity() {
		t.Fatal("unexpected mistral capabilities")
	}
}
