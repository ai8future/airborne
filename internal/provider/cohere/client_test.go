package cohere

import "testing"

func TestClientConfiguration(t *testing.T) {
	client := NewClient(nil, WithDebugLogging(true))
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if got := client.Name(); got != "cohere" {
		t.Fatalf("Name() = %q, want %q", got, "cohere")
	}
	if !client.SupportsStreaming() {
		t.Fatal("provider must support streaming")
	}
	if client.SupportsNativeContinuity() {
		t.Fatal("OpenAI-compatible adapter must not claim native continuity")
	}
	if got := client.SupportsWebSearch(); got != true {
		t.Fatalf("SupportsWebSearch() = %v, want true", got)
	}
	if client.SupportsFileSearch() {
		t.Fatal("provider must not claim file search")
	}
}
