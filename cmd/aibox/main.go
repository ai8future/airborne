package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/cliffpyles/aibox/internal/config"
	"github.com/cliffpyles/aibox/internal/server"
	"google.golang.org/grpc"
)

// Build-time variables
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Set up structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Log startup info
	slog.Info("starting AIBox",
		"version", Version,
		"commit", GitCommit,
		"build_time", BuildTime,
		"grpc_port", cfg.Server.GRPCPort,
	)

	// Create gRPC server
	grpcServer, err := server.NewGRPCServer(cfg, server.VersionInfo{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
	})
	if err != nil {
		slog.Error("failed to create gRPC server", "error", err)
		os.Exit(1)
	}

	// Start listening
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.GRPCPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("failed to listen", "address", addr, "error", err)
		os.Exit(1)
	}

	// Handle shutdown gracefully
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start server in goroutine
	go func() {
		slog.Info("gRPC server listening", "address", addr)
		if err := grpcServer.Serve(listener); err != nil && err != grpc.ErrServerStopped {
			slog.Error("gRPC server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	slog.Info("shutdown signal received, stopping server...")

	// Graceful shutdown
	grpcServer.GracefulStop()
	slog.Info("server stopped")
}
