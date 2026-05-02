package redis

import (
	"context"
	"testing"

	"github.com/ai8future/chassis-go-addons/rediskit"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestClient(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := Config{
		Addr: mr.Addr(),
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Test Set and Get
	err = client.Set(ctx, "key", "value", 0)
	if err != nil {
		t.Errorf("Set failed: %v", err)
	}

	val, err := client.Get(ctx, "key")
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}
	if val != "value" {
		t.Errorf("expected 'value', got %q", val)
	}

	// Test Exists
	exists, err := client.Exists(ctx, "key")
	if err != nil {
		t.Errorf("Exists failed: %v", err)
	}
	if exists != 1 {
		t.Errorf("expected exists 1, got %d", exists)
	}

	// Test Del
	err = client.Del(ctx, "key")
	if err != nil {
		t.Errorf("Del failed: %v", err)
	}

	_, err = client.Get(ctx, "key")
	if !IsNil(err) {
		t.Errorf("expected nil error after Del, got %v", err)
	}
}

func TestIsNilRecognizesRedisNotFoundErrors(t *testing.T) {
	if !IsNil(rediskit.ErrNotFound) {
		t.Fatal("expected rediskit.ErrNotFound to be treated as redis nil")
	}

	if !IsNil(goredis.Nil) {
		t.Fatal("expected go-redis Nil to be treated as redis nil")
	}
}
