package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	airbornev1 "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/admin"
	"github.com/ai8future/airborne/internal/config"
	"github.com/ai8future/airborne/internal/markdownsvc"
	"github.com/ai8future/airborne/internal/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Build-time variables
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Parse command-line flags
	healthCheck := flag.Bool("health-check", false, "Run gRPC health check and exit")
	flag.Parse()

	// If health check mode, run the check and exit
	if *healthCheck {
		if err := runHealthCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Load configuration first so we can configure logging from it
	cfg, err := config.Load()
	if err != nil {
		// Use a basic logger for this error since config isn't loaded yet
		fmt.Fprintf(os.Stderr, "failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Configure logging based on config
	configureLogger(cfg.Logging)

	// Log startup info
	slog.Info("starting Airborne",
		"version", Version,
		"commit", GitCommit,
		"build_time", BuildTime,
		"grpc_port", cfg.Server.GRPCPort,
	)

	// Initialize markdown_svc client (optional service)
	if err := markdownsvc.Initialize(cfg.MarkdownSvcAddr); err != nil {
		slog.Error("markdownsvc init failed", "error", err)
	}
	defer markdownsvc.Close()

	// Create gRPC server
	grpcServer, components, err := server.NewGRPCServer(cfg, server.VersionInfo{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
	})
	if err != nil {
		slog.Error("failed to create gRPC server", "error", err)
		os.Exit(1)
	}
	defer components.Close()

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

	// Start gRPC server in goroutine
	go func() {
		slog.Info("gRPC server listening", "address", addr)
		if err := grpcServer.Serve(listener); err != nil && err != grpc.ErrServerStopped {
			slog.Error("gRPC server error", "error", err)
			os.Exit(1)
		}
	}()

	// Start admin HTTP server if enabled
	var adminServer *admin.Server
	if cfg.Admin.Enabled {
		// Build gRPC address for the test endpoint
		grpcHost := cfg.Server.Host
		if grpcHost == "" || grpcHost == "0.0.0.0" {
			grpcHost = "127.0.0.1"
		}
		grpcAddr := fmt.Sprintf("%s:%d", grpcHost, cfg.Server.GRPCPort)

		adminServer = admin.NewServer(components.Repository, admin.Config{
			Port:      cfg.Admin.Port,
			GRPCAddr:  grpcAddr,
			AuthToken: cfg.Auth.AdminToken,
		})
		go func() {
			if err := adminServer.Start(); err != nil && err != http.ErrServerClosed {
				slog.Error("admin server error", "error", err)
			}
		}()
	}

	// Wait for shutdown signal
	<-ctx.Done()
	slog.Info("shutdown signal received, stopping servers...")

	// Graceful shutdown
	if adminServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := adminServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("admin server shutdown error", "error", err)
		}
	}
	grpcServer.GracefulStop()
	slog.Info("servers stopped")
}

// configureLogger sets up the default slog logger based on config values
func configureLogger(cfg config.LoggingConfig) {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}

// runHealthCheck performs a gRPC health check against the AdminService/Health endpoint
func runHealthCheck() error {
	// Load configuration to get server address and TLS settings
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Build the address, using 127.0.0.1 if host is empty or 0.0.0.0
	host := cfg.Server.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, cfg.Server.GRPCPort)

	// Create context with 3 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Set up credentials based on TLS configuration
	var dialOpts []grpc.DialOption
	if cfg.TLS.Enabled {
		creds, err := credentials.NewClientTLSFromFile(cfg.TLS.CertFile, "")
		if err != nil {
			return fmt.Errorf("failed to create TLS credentials: %w", err)
		}
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Dial the gRPC server
	conn, err := grpc.DialContext(ctx, addr, dialOpts...)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	// Create AdminService client and call Health
	client := airbornev1.NewAdminServiceClient(conn)
	resp, err := client.Health(ctx, &airbornev1.HealthRequest{})
	if err != nil {
		return fmt.Errorf("health check RPC failed: %w", err)
	}

	// Check if status is healthy
	if resp.Status != "healthy" {
		return fmt.Errorf("server unhealthy: status=%s", resp.Status)
	}

	return nil
}
