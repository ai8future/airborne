package server

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

// VersionInfo contains build version information
type VersionInfo struct {
	Version   string
	GitCommit string
	BuildTime string
}

// ServerComponents holds components needed by both gRPC and admin servers
type ServerComponents struct {
	KeyStore    *auth.KeyStore
	RateLimiter *auth.RateLimiter
	TenantMgr   *tenant.Manager
	RedisClient *redis.Client
	DBClient    *db.Client
	Repository  *db.Repository
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

	// Initialize auth based on mode
	var redisClient *redis.Client
	var keyStore *auth.KeyStore
	var rateLimiter *auth.RateLimiter
	var tenantInterceptor *auth.TenantInterceptor

	if cfg.Auth.AuthMode == "redis" {
		// Redis-based auth (existing behavior)
		redisClient, err = redis.NewClient(redis.Config{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("redis required for auth_mode=redis: %w", err)
		}
		keyStore = auth.NewKeyStore(redisClient)
		rateLimiter = auth.NewRateLimiter(redisClient, auth.RateLimits{
			RequestsPerMinute: cfg.RateLimits.DefaultRPM,
			RequestsPerDay:    cfg.RateLimits.DefaultRPD,
			TokensPerMinute:   cfg.RateLimits.DefaultTPM,
		}, true)
		slog.Info("using Redis-based authentication")
	} else {
		// Static token auth (default)
		if cfg.Auth.AdminToken == "" {
			return nil, nil, fmt.Errorf("AIRBORNE_ADMIN_TOKEN required for static auth mode")
		}
		slog.Info("using static token authentication (no Redis)")
	}

	// Create tenant interceptor if tenant manager is available
	if tenantMgr != nil {
		tenantInterceptor = auth.NewTenantInterceptor(tenantMgr)
	}

	// Build interceptor chains
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		recoveryInterceptor(),
		loggingInterceptor(),
	}
	streamInterceptors := []grpc.StreamServerInterceptor{
		streamRecoveryInterceptor(),
		streamLoggingInterceptor(),
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

	// Initialize database if enabled
	var dbClient *db.Client
	var repo *db.Repository
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
			repo = db.NewRepository(dbClient)
			slog.Info("database connection established for message persistence")
		}
	}

	// Register services
	chatService := service.NewChatService(rateLimiter, ragService, imageGenClient, repo)
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

	components := &ServerComponents{
		KeyStore:    keyStore,
		RateLimiter: rateLimiter,
		TenantMgr:   tenantMgr,
		RedisClient: redisClient,
		DBClient:    dbClient,
		Repository:  repo,
	}

	return server, components, nil
}

// Close closes all server components that need cleanup.
func (c *ServerComponents) Close() {
	if c.DBClient != nil {
		c.DBClient.Close()
	}
}

// recoveryInterceptor recovers from panics in unary handlers
func recoveryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				// Log stack trace
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				slog.Error("panic recovered",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(buf[:n]),
				)
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// streamRecoveryInterceptor recovers from panics in stream handlers
func streamRecoveryInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				slog.Error("panic recovered in stream",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(buf[:n]),
				)
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}

// loggingInterceptor logs unary requests
func loggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		code := codes.OK
		if err != nil {
			if st, ok := status.FromError(err); ok {
				code = st.Code()
			} else {
				code = codes.Unknown
			}
		}

		// Skip logging for health checks
		if info.FullMethod != "/airborne.v1.AdminService/Health" {
			slog.Info("gRPC request",
				"method", info.FullMethod,
				"duration_ms", duration.Milliseconds(),
				"code", code.String(),
			)
		}

		return resp, err
	}
}

// streamLoggingInterceptor logs stream requests
func streamLoggingInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		err := handler(srv, ss)

		duration := time.Since(start)
		code := codes.OK
		if err != nil {
			if st, ok := status.FromError(err); ok {
				code = st.Code()
			} else {
				code = codes.Unknown
			}
		}

		slog.Info("gRPC stream",
			"method", info.FullMethod,
			"duration_ms", duration.Milliseconds(),
			"code", code.String(),
		)

		return err
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
