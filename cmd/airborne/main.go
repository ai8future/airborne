package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	chassis "github.com/ai8future/chassis-go/v10"
	"github.com/ai8future/chassis-go/v10/deploy"
	"github.com/ai8future/chassis-go/v10/kafkakit"
	"github.com/ai8future/chassis-go/v10/lifecycle"
	"github.com/ai8future/chassis-go/v10/logz"
	otelinit "github.com/ai8future/chassis-go/v10/otel"
	"github.com/ai8future/chassis-go/v10/registry"
	"github.com/ai8future/chassis-go/v10/xyops"

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
	chassis.RequireMajor(10)

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

	// Discover deploy directory and load env files before config
	d := deploy.Discover("airborne")
	d.LoadEnv()

	// Load configuration first so we can configure logging from it
	cfg, err := config.Load()
	if err != nil {
		// Use a basic logger for this error since config isn't loaded yet
		fmt.Fprintf(os.Stderr, "failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Configure structured JSON logging with trace ID support
	logger := logz.New(cfg.Logging.Level)
	slog.SetDefault(logger)

	if d.Found() {
		env := d.Environment()
		slog.Info("deploy", "dir", d.Dir(), "runtime", env.Runtime, "env", env.Env)
	}

	// Initialize OpenTelemetry tracing and metrics
	otelShutdown := otelinit.Init(otelinit.Config{
		ServiceName:    "airborne",
		ServiceVersion: Version,
		Insecure:       true, // use plaintext for local/dev
	})
	defer otelShutdown(context.Background())

	// Log startup info
	slog.Info("starting Airborne",
		"version", Version,
		"commit", GitCommit,
		"build_time", BuildTime,
		"grpc_port", cfg.Server.GRPCPort,
		"chassis_version", chassis.Version,
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

	// Build admin server if enabled (created before lifecycle.Run so it's available to components)
	var adminServer *admin.Server
	if cfg.Admin.Enabled {
		grpcHost := cfg.Server.Host
		if grpcHost == "" || grpcHost == "0.0.0.0" {
			grpcHost = "127.0.0.1"
		}
		grpcAddr := fmt.Sprintf("%s:%d", grpcHost, cfg.Server.GRPCPort)

		adminServer = admin.NewServer(components.DBClient, admin.Config{
			Port:         cfg.Admin.Port,
			GRPCAddr:     grpcAddr,
			AuthToken:    cfg.Auth.AdminToken,
			TenantMgr:    components.TenantMgr,
			RedisClient:  components.RedisClient,
			HealthChecks: components.HealthChecks,
			Version: admin.VersionInfo{
				Version:   Version,
				GitCommit: GitCommit,
				BuildTime: BuildTime,
			},
		})
	}

	// Create kafkakit publisher for event bus (optional — degrades if unconfigured)
	kafkaCfg := cfg.Kafkakit.ToKafkakit()
	if kafkaCfg.Enabled() {
		pub, err := kafkakit.NewPublisher(kafkaCfg)
		if err != nil {
			slog.Warn("kafkakit publisher failed to create, events disabled", "error", err)
		} else {
			defer pub.Close()
			components.ChatService.SetEventPublisher(pub)
			slog.Info("kafkakit event bus enabled")
		}
	}

	// Declare ports for registry visibility (before lifecycle.Run)
	registry.Port(chassis.PortGRPC, cfg.Server.GRPCPort, "gRPC API")
	if cfg.Admin.Enabled {
		registry.Port(chassis.PortHTTP, cfg.Admin.Port, "Admin HTTP")
	}

	// Create xyops client for monitoring bridge (optional — degrades if unconfigured)
	var ops *xyops.Client
	xyopsCfg := cfg.Xyops.ToXyops()
	if xyopsCfg.BaseURL != "" && xyopsCfg.APIKey != "" {
		ops = xyops.New(xyopsCfg, xyops.WithMonitoring(xyopsCfg.MonitorInterval))
		slog.Info("xyops monitoring enabled", "base_url", xyopsCfg.BaseURL)
	}

	// Use lifecycle.Run for coordinated startup and shutdown.
	// Catches SIGTERM/SIGINT, cancels context, waits for all components.
	lifecycleArgs := []any{
		// Enable kafkakit event bus (heartbeatkit + announcekit auto-activate)
		lifecycle.WithKafkaConfig(cfg.Kafkakit.ToKafkakit()),

		// gRPC server component
		lifecycle.Component(func(ctx context.Context) error {
			slog.Info("gRPC server listening", "address", addr)
			errCh := make(chan error, 1)
			go func() { errCh <- grpcServer.Serve(listener) }()
			select {
			case <-ctx.Done():
				grpcServer.GracefulStop()
				return nil
			case err := <-errCh:
				if err == grpc.ErrServerStopped {
					return nil
				}
				return err
			}
		}),
	}

	// Admin HTTP server component (only if enabled)
	if adminServer != nil {
		lifecycleArgs = append(lifecycleArgs, lifecycle.Component(func(ctx context.Context) error {
			errCh := make(chan error, 1)
			go func() { errCh <- adminServer.Start() }()
			select {
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return adminServer.Shutdown(shutdownCtx)
			case err := <-errCh:
				if err == http.ErrServerClosed {
					return nil
				}
				return err
			}
		}))
	}

	// Wire xyops monitoring bridge into lifecycle (blocks until ctx cancellation)
	if ops != nil {
		lifecycleArgs = append(lifecycleArgs, ops.Run)
	}

	if err := lifecycle.Run(context.Background(), lifecycleArgs...); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
	slog.Info("servers stopped")
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
