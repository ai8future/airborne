package server

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"github.com/ai8future/chassis-go/v11/grpckit"

	pb "github.com/ai8future/airborne/gen/go/airborne/v1"
	"github.com/ai8future/airborne/internal/auth"
	"github.com/ai8future/airborne/internal/config"
	"github.com/ai8future/airborne/internal/db"
	"github.com/ai8future/airborne/internal/imagegen"
	"github.com/ai8future/airborne/internal/rag"
	"github.com/ai8future/airborne/internal/rag/embedder"
	"github.com/ai8future/airborne/internal/rag/extractor"
	"github.com/ai8future/airborne/internal/rag/vectorstore"
	"github.com/ai8future/airborne/internal/redis"
	"github.com/ai8future/airborne/internal/service"
	"github.com/ai8future/airborne/internal/tenant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

// VersionInfo contains build version information
type VersionInfo struct {
	Version   string
	GitCommit string
	BuildTime string
}

// ServerComponents holds components needed by both gRPC and admin servers
type ServerComponents struct {
	KeyStore     *auth.KeyStore
	RateLimiter  *auth.RateLimiter
	TenantMgr    *tenant.Manager
	RedisClient  *redis.Client
	DBClient     *db.Client
	HealthChecks map[string]func(context.Context) error // Additional health checks (e.g., RAG dependencies)
	ChatService  *service.ChatService                   // Exposed for event publisher wiring
}

// NewGRPCServer creates a new gRPC server with all services registered
// Returns the server and components needed by admin HTTP server
func NewGRPCServer(cfg *config.Config, version VersionInfo) (*grpc.Server, *ServerComponents, error) {
	// Load tenant configurations
	tenantMgr, err := tenant.Load("")
	if err != nil {
		slog.Warn("tenant config not loaded - running in single-tenant legacy mode", "error", err)
		// Create an empty manager for legacy mode
		tenantMgr = nil
	} else {
		slog.Info("tenant configurations loaded",
			"tenant_count", tenantMgr.TenantCount(),
			"tenants", tenantMgr.TenantCodes(),
		)
	}

	// Redis-backed idempotency is independent of the authentication mode. Static
	// auth may deliberately start without Redis for unkeyed requests, but Redis
	// auth cannot operate without it.
	var redisClient *redis.Client
	var keyStore *auth.KeyStore
	var rateLimiter *auth.RateLimiter
	var tenantInterceptor *auth.TenantInterceptor
	var dbClient *db.Client
	initialized := false
	defer func() {
		if initialized {
			return
		}
		if dbClient != nil {
			dbClient.Close()
		}
		if redisClient != nil {
			_ = redisClient.Close()
		}
	}()

	if cfg.Auth.AuthMode != "redis" && cfg.Auth.AdminToken == "" {
		return nil, nil, fmt.Errorf("AIRBORNE_ADMIN_TOKEN required for static auth mode")
	}

	if cfg.Redis.Addr != "" {
		redisClient, err = redis.NewClient(redis.Config{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err != nil {
			redisClient = nil
			if cfg.Auth.AuthMode == "redis" {
				return nil, nil, fmt.Errorf("redis required for auth_mode=redis: %w", err)
			}
			slog.Warn("configured Redis unavailable - continuing static auth for unkeyed requests; keyed requests remain fail-closed", "error", err)
		} else {
			slog.Info("Redis connection established for keyed idempotency")
		}
	} else if cfg.Auth.AuthMode == "redis" {
		return nil, nil, fmt.Errorf("redis required for auth_mode=redis: address is empty")
	}

	if cfg.Auth.AuthMode == "redis" {
		keyStore = auth.NewKeyStore(redisClient)
		rateLimiter = auth.NewRateLimiter(redisClient, auth.RateLimits{
			RequestsPerMinute: cfg.RateLimits.DefaultRPM,
			RequestsPerDay:    cfg.RateLimits.DefaultRPD,
			TokensPerMinute:   cfg.RateLimits.DefaultTPM,
		}, true)
		slog.Info("using Redis-based authentication")
	} else {
		if redisClient != nil {
			slog.Info("using static token authentication with Redis-backed keyed idempotency")
		} else {
			slog.Info("using static token authentication without Redis; only unkeyed requests are available")
		}
	}

	// Initialize database if enabled (before the tenant interceptor so it can
	// validate tenant IDs against the airborne_tenants registry).
	if cfg.Database.Enabled {
		var dbErr error
		dbClient, dbErr = db.NewClient(context.Background(), db.Config{
			URL:            cfg.Database.URL,
			MaxConnections: cfg.Database.MaxConnections,
			LogQueries:     cfg.Database.LogQueries,
			CACert:         cfg.Database.CACert,
		})
		if dbErr != nil {
			slog.Error("failed to connect to database", "error", dbErr)
			// Continue without database - it's optional
		} else {
			slog.Info("database connection established for message persistence")
		}
	}

	// Create tenant interceptor if tenant manager is available
	if tenantMgr != nil {
		tenantInterceptor = auth.NewTenantInterceptor(tenantMgr, dbClient)
	}

	// Build interceptor chains using chassis-go grpckit
	logger := slog.Default()
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		grpckit.UnaryRecovery(logger),
		grpckit.UnaryTracing(),
		grpckit.UnaryMetrics(),
		skipHealthLogging(grpckit.UnaryLogging(logger)),
	}
	streamInterceptors := []grpc.StreamServerInterceptor{
		grpckit.StreamRecovery(logger),
		grpckit.StreamTracing(),
		grpckit.StreamMetrics(),
		grpckit.StreamLogging(logger),
	}

	// Add tenant interceptor first (validates tenant before auth)
	if tenantInterceptor != nil {
		unaryInterceptors = append(unaryInterceptors, tenantInterceptor.UnaryInterceptor())
		streamInterceptors = append(streamInterceptors, tenantInterceptor.StreamInterceptor())
	}

	// Add auth interceptors based on mode
	if cfg.Auth.AuthMode == "redis" && keyStore != nil {
		authenticator := auth.NewAuthenticator(keyStore, rateLimiter)
		unaryInterceptors = append(unaryInterceptors, authenticator.UnaryInterceptor())
		streamInterceptors = append(streamInterceptors, authenticator.StreamInterceptor())
	} else if cfg.Auth.AuthMode != "redis" {
		// Static token auth
		staticAuth := auth.NewStaticAuthenticator(cfg.Auth.AdminToken)
		unaryInterceptors = append(unaryInterceptors, staticAuth.UnaryInterceptor())
		streamInterceptors = append(streamInterceptors, staticAuth.StreamInterceptor())
	}

	// Build server options
	opts := []grpc.ServerOption{
		// Keepalive settings
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 5 * time.Minute,
			Time:                  5 * time.Minute,
			Timeout:               20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),

		// Interceptors
		grpc.ChainUnaryInterceptor(unaryInterceptors...),
		grpc.ChainStreamInterceptor(streamInterceptors...),

		// Message size limits (100MB for file uploads)
		grpc.MaxRecvMsgSize(100 * 1024 * 1024),
		grpc.MaxSendMsgSize(100 * 1024 * 1024),
	}

	// Add TLS if enabled
	if cfg.TLS.Enabled {
		creds, err := credentials.NewServerTLSFromFile(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, nil, err
		}
		opts = append(opts, grpc.Creds(creds))
	}

	// Create server
	server := grpc.NewServer(opts...)

	// Initialize RAG service if enabled (before ChatService so it can use it)
	var ragService *rag.Service
	if cfg.RAG.Enabled {
		// Initialize RAG components
		emb := embedder.NewOllamaEmbedder(embedder.OllamaConfig{
			BaseURL: cfg.RAG.OllamaURL,
			Model:   cfg.RAG.EmbeddingModel,
		})

		store := vectorstore.NewQdrantStore(vectorstore.QdrantConfig{
			BaseURL: cfg.RAG.QdrantURL,
		})

		ext := extractor.NewDocboxExtractor(extractor.DocboxConfig{
			BaseURL: cfg.RAG.DocboxURL,
		})

		ragService = rag.NewService(emb, store, ext, rag.ServiceOptions{
			ChunkSize:     cfg.RAG.ChunkSize,
			ChunkOverlap:  cfg.RAG.ChunkOverlap,
			RetrievalTopK: cfg.RAG.RetrievalTopK,
		})

		slog.Info("RAG enabled",
			"ollama_url", cfg.RAG.OllamaURL,
			"embedding_model", cfg.RAG.EmbeddingModel,
			"qdrant_url", cfg.RAG.QdrantURL,
			"docbox_url", cfg.RAG.DocboxURL,
		)
	}

	// Create image generation client
	imageGenClient := imagegen.NewClient()

	// Register services
	// redisClient may be nil (auth_mode != "redis"): unkeyed requests remain
	// available, while keyed GenerateReply requests fail closed before dispatch.
	chatService := service.NewChatService(
		rateLimiter,
		ragService,
		imageGenClient,
		dbClient,
		redisClient,
		cfg.Idempotency.CompletedResponseRetention,
	)
	pb.RegisterAirborneServiceServer(server, chatService)

	adminService := service.NewAdminService(redisClient, service.AdminServiceConfig{
		Version:   version.Version,
		GitCommit: version.GitCommit,
		BuildTime: version.BuildTime,
		GoVersion: runtime.Version(),
	})
	pb.RegisterAdminServiceServer(server, adminService)

	// Register FileService if RAG is enabled
	if ragService != nil {
		fileService := service.NewFileService(ragService, rateLimiter)
		pb.RegisterFileServiceServer(server, fileService)
	}

	// Register standard gRPC health service via chassis-go
	grpckit.RegisterHealth(server, func(ctx context.Context) error {
		return checkServerDependencies(ctx, dbClient, redisClient)
	})

	tenantCount := 0
	if tenantMgr != nil {
		tenantCount = tenantMgr.TenantCount()
	}
	slog.Info("gRPC server created",
		"tls_enabled", cfg.TLS.Enabled,
		"auth_mode", cfg.Auth.AuthMode,
		"multitenancy_enabled", tenantInterceptor != nil,
		"tenant_count", tenantCount,
		"version", version.Version,
	)

	// Build additional health checks for RAG dependencies
	extraHealthChecks := make(map[string]func(context.Context) error)
	if ragService != nil {
		extraHealthChecks["qdrant"] = ragService.PingVectorStore
		extraHealthChecks["ollama"] = ragService.PingEmbedder
	}

	components := &ServerComponents{
		KeyStore:     keyStore,
		RateLimiter:  rateLimiter,
		TenantMgr:    tenantMgr,
		RedisClient:  redisClient,
		DBClient:     dbClient,
		HealthChecks: extraHealthChecks,
		ChatService:  chatService,
	}

	initialized = true
	return server, components, nil
}

func checkServerDependencies(ctx context.Context, dbClient *db.Client, redisClient *redis.Client) error {
	if dbClient != nil {
		if err := dbClient.Ping(ctx); err != nil {
			return fmt.Errorf("database readiness: %w", err)
		}
	}
	if redisClient != nil {
		if err := redisClient.Ping(ctx); err != nil {
			return fmt.Errorf("redis readiness: %w", err)
		}
	}
	return nil
}

// Close closes all server components that need cleanup.
func (c *ServerComponents) Close() {
	if c.DBClient != nil {
		c.DBClient.Close()
	}
	if c.RedisClient != nil {
		_ = c.RedisClient.Close()
	}
}

// skipHealthLogging wraps a unary logging interceptor to skip health check methods,
// preventing log noise from frequent probes.
func skipHealthLogging(inner grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if strings.HasSuffix(info.FullMethod, "/Health") || strings.HasSuffix(info.FullMethod, "/Check") {
			return handler(ctx, req)
		}
		return inner(ctx, req, info, handler)
	}
}

// developmentAuthInterceptor injects a dev client in non-production mode when Redis is unavailable.
//
// WARNING: This function bypasses authentication entirely. It is intended ONLY for
// local development and testing. NEVER wire this into NewGRPCServer for production builds.
// If you need to use this, ensure it's behind a build tag or explicit development mode check.
func developmentAuthInterceptor() grpc.UnaryServerInterceptor {
	slog.Warn("SECURITY: Development authentication interceptor is active - do not use in production")
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		client := &auth.ClientKey{
			ClientID:   "dev",
			ClientName: "development",
			Permissions: []auth.Permission{
				// NOTE: PermissionAdmin intentionally excluded for security
				auth.PermissionChat,
				auth.PermissionChatStream,
				auth.PermissionFiles,
			},
		}
		ctx = context.WithValue(ctx, auth.ClientContextKey, client)
		return handler(ctx, req)
	}
}

// developmentAuthStreamInterceptor injects a dev client for streams in non-production mode.
//
// WARNING: This function bypasses authentication entirely. It is intended ONLY for
// local development and testing. NEVER wire this into NewGRPCServer for production builds.
// If you need to use this, ensure it's behind a build tag or explicit development mode check.
func developmentAuthStreamInterceptor() grpc.StreamServerInterceptor {
	slog.Warn("SECURITY: Development stream authentication interceptor is active - do not use in production")
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		client := &auth.ClientKey{
			ClientID:   "dev",
			ClientName: "development",
			Permissions: []auth.Permission{
				// NOTE: PermissionAdmin intentionally excluded for security
				auth.PermissionChat,
				auth.PermissionChatStream,
				auth.PermissionFiles,
			},
		}
		ctx := context.WithValue(ss.Context(), auth.ClientContextKey, client)
		wrapped := &devWrappedStream{ServerStream: ss, ctx: ctx}
		return handler(srv, wrapped)
	}
}

type devWrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *devWrappedStream) Context() context.Context {
	return s.ctx
}
