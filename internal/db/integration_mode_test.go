package db

import (
	"strings"
	"testing"
)

func TestIntegrationRequired(t *testing.T) {
	t.Setenv("AIRBORNE_REQUIRE_INTEGRATION", "")
	if integrationRequired() {
		t.Fatal("unset integration requirement must preserve fast-suite skipping")
	}

	t.Setenv("AIRBORNE_REQUIRE_INTEGRATION", "1")
	if !integrationRequired() {
		t.Fatal("AIRBORNE_REQUIRE_INTEGRATION=1 must require Docker-backed DB tests")
	}

	t.Setenv("AIRBORNE_REQUIRE_INTEGRATION", "true")
	if integrationRequired() {
		t.Fatal("only the explicit value 1 enables the required integration gate")
	}
}

func TestIntegrationPostgresImageIsDigestPinned(t *testing.T) {
	if !strings.Contains(integrationPostgresImage, "@sha256:") {
		t.Fatalf("integration Postgres image must be digest-pinned, got %q", integrationPostgresImage)
	}
}
