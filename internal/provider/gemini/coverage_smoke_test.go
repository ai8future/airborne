package gemini

import (
	"context"
	"github.com/ai8future/airborne/internal/provider"
	"testing"
)

func TestCoverageCapabilitiesAndMissingKey(t *testing.T) {
	c := NewClient(nil, WithDebugLogging(true))
	if c.Name() == "" || !c.SupportsStreaming() {
		t.Fatal("capabilities")
	}
	if _, err := c.GenerateReply(context.Background(), provider.GenerateParams{}); err == nil {
		t.Fatal("missing key must fail")
	}
}
