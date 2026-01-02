package server

import (
	"testing"

	"github.com/cliffpyles/aibox/internal/config"
)

func TestNewGRPCServer_FailsWithoutRedisInProductionMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		StartupMode: config.StartupModeProduction,
		Redis: config.RedisConfig{
			Addr: "invalid:6379", // Will fail to connect
		},
	}

	_, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err == nil {
		t.Fatal("expected error when Redis unavailable in production mode")
	}
}

func TestNewGRPCServer_AllowsNoRedisInDevelopmentMode(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 50051,
			Host:     "127.0.0.1",
		},
		StartupMode: config.StartupModeDevelopment,
		Redis: config.RedisConfig{
			Addr: "invalid:6379", // Will fail to connect
		},
	}

	server, err := NewGRPCServer(cfg, VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("development mode should allow missing Redis: %v", err)
	}
	if server == nil {
		t.Fatal("server should not be nil")
	}
	server.Stop()
}
