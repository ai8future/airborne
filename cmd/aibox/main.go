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

	aiboxv1 "github.com/cliffpyles/aibox/gen/go/aibox/v1"
	"github.com/cliffpyles/aibox/internal/admin"
	"github.com/cliffpyles/aibox/internal/config"
	"github.com/cliffpyles/aibox/internal/server"
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
	slog.Info("starting AIBox",
		"version", Version,
		"commit", GitCommit,
		"build_time", BuildTime,
		"grpc_port", cfg.Server.GRPCPort,
	)

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

	// Start Admin HTTP server if enabled
	var adminServer *http.Server
	if cfg.Server.AdminPort > 0 {
		// Initialize admin auth - determine data directory
		dataDir := os.Getenv("AIBOX_DATA_DIR")
		if dataDir == "" {
			// In Docker, use /app/data; otherwise use ~/.aibox
			if _, err := os.Stat("/app/data"); err == nil {
				dataDir = "/app/data"
			} else if homeDir, err := os.UserHomeDir(); err == nil {
				dataDir = homeDir + "/.aibox"
			} else {
				dataDir = "/tmp/aibox"
			}
		}
		adminAuth := admin.NewAdminAuth(dataDir)
		if err := adminAuth.Load(); err != nil {
			slog.Error("failed to load admin credentials", "error", err)
		}
		adminAuth.StartCleanupRoutine()

		// Create admin server
		adminHTTP := admin.NewServer(
			cfg,
			adminAuth,
			components.KeyStore,
			components.RateLimiter,
			components.TenantMgr,
			admin.VersionInfo{
				Version:   Version,
				GitCommit: GitCommit,
				BuildTime: BuildTime,
			},
		)

		adminAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.AdminPort)
		adminServer = &http.Server{
			Addr:    adminAddr,
			Handler: adminHTTP,
		}

		go func() {
			slog.Info("admin HTTP server listening", "address", adminAddr)
			if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("admin HTTP server error", "error", err)
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
		adminServer.Shutdown(shutdownCtx)
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
	client := aiboxv1.NewAdminServiceClient(conn)
	resp, err := client.Health(ctx, &aiboxv1.HealthRequest{})
	if err != nil {
		return fmt.Errorf("health check RPC failed: %w", err)
	}

	// Check if status is healthy
	if resp.Status != "healthy" {
		return fmt.Errorf("server unhealthy: status=%s", resp.Status)
	}

	return nil
}
