package markdownsvc

import (
	"testing"
	"time"
)

func TestDefaultOperationTimeout(t *testing.T) {
	if defaultOperationTimeout != 120*time.Second {
		t.Fatalf("default operation timeout = %s, want 120s", defaultOperationTimeout)
	}
}
