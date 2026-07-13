package redis

import (
	"context"
	"sort"
	"testing"
	"time"

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

func TestClientExtendedCommands(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := NewClient(Config{Addr: mr.Addr()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if set, err := client.SetNX(ctx, "once", "first", 0); err != nil || !set {
		t.Fatalf("first SetNX = %v, %v; want true, nil", set, err)
	}
	if set, err := client.SetNX(ctx, "once", "second", 0); err != nil || set {
		t.Fatalf("second SetNX = %v, %v; want false, nil", set, err)
	}
	if got, err := client.Incr(ctx, "counter"); err != nil || got != 1 {
		t.Fatalf("Incr = %d, %v", got, err)
	}
	if got, err := client.IncrBy(ctx, "counter", 4); err != nil || got != 5 {
		t.Fatalf("IncrBy = %d, %v", got, err)
	}
	if err := client.Expire(ctx, "counter", time.Minute); err != nil {
		t.Fatal(err)
	}
	if ttl, err := client.TTL(ctx, "counter"); err != nil || ttl <= 0 {
		t.Fatalf("TTL = %v, %v", ttl, err)
	}
	if got, err := client.Eval(ctx, "return ARGV[1]", nil, "script-result"); err != nil || got != "script-result" {
		t.Fatalf("Eval = %#v, %v", got, err)
	}

	if err := client.HSet(ctx, "hash", "first", "one", "second", "two"); err != nil {
		t.Fatal(err)
	}
	if got, err := client.HGet(ctx, "hash", "first"); err != nil || got != "one" {
		t.Fatalf("HGet = %q, %v", got, err)
	}
	if got, err := client.HGetAll(ctx, "hash"); err != nil || got["second"] != "two" {
		t.Fatalf("HGetAll = %#v, %v", got, err)
	}
	if err := client.HDel(ctx, "hash", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.HGet(ctx, "hash", "first"); !IsNil(err) {
		t.Fatalf("HGet deleted field error = %v, want redis nil", err)
	}

	if err := client.Set(ctx, "prefix:a", "a", 0); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, "prefix:b", "b", 0); err != nil {
		t.Fatal(err)
	}
	keys, err := client.Scan(ctx, "prefix:*")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(keys)
	if len(keys) != 2 || keys[0] != "prefix:a" || keys[1] != "prefix:b" {
		t.Fatalf("Scan = %#v", keys)
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
