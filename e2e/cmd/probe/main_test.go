package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseOptionsRequiresIdempotencyKey(t *testing.T) {
	_, err := parseOptions([]string{"--token", "test-token"})
	if err == nil || !strings.Contains(err.Error(), "idempotency-key is required") {
		t.Fatalf("parseOptions() error = %v, want required idempotency key error", err)
	}
}

func TestGenerateReplyRequestKeepsRequestAndIdempotencyKeysSeparate(t *testing.T) {
	options, err := parseOptions([]string{
		"--token", "test-token",
		"--request-id", "22222222-2222-4222-8222-222222222222",
		"--idempotency-key", "airborne-e2e-replay-v1",
	})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	request := newGenerateReplyRequest(options)
	if got, want := request.GetRequestId(), "22222222-2222-4222-8222-222222222222"; got != want {
		t.Fatalf("RequestId = %q, want %q", got, want)
	}
	if got, want := request.GetIdempotencyKey(), "airborne-e2e-replay-v1"; got != want {
		t.Fatalf("IdempotencyKey = %q, want %q", got, want)
	}
	if request.GetRequestId() == request.GetIdempotencyKey() {
		t.Fatal("request ID and idempotency key must remain separate")
	}
}

func TestReplayProbeCallsUseSameExplicitIdempotencyKey(t *testing.T) {
	content, err := os.ReadFile("../../run.sh")
	if err != nil {
		t.Fatalf("read e2e/run.sh: %v", err)
	}

	const expectedFlag = `--idempotency-key "$grpc_idempotency_key"`
	if got := strings.Count(string(content), expectedFlag); got != 2 {
		t.Fatalf("explicit replay idempotency flag count = %d, want 2", got)
	}
	if !strings.Contains(string(content), "grpc_idempotency_key=airborne-e2e-grpc-replay-v1") {
		t.Fatal("e2e/run.sh must define one deterministic replay idempotency key")
	}
}
