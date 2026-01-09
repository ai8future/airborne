package server

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
	"github.com/cliffpyles/aibox/internal/auth"
	"github.com/cliffpyles/aibox/internal/config"
	"github.com/cliffpyles/aibox/internal/rag"
	"github.com/cliffpyles/aibox/internal/rag/embedder"
	"github.com/cliffpyles/aibox/internal/rag/extractor"
	"github.com/cliffpyles/aibox/internal/rag/vectorstore"
	"github.com/cliffpyles/aibox/internal/redis"
	"github.com/cliffpyles/aibox/internal/service"
	"github.com/cliffpyles/aibox/internal/tenant"
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

	// Initialize Redis (optional - graceful degradation if not available)
	var redisClient *redis.Client
	var keyStore *auth.KeyStore
	var rateLimiter *auth.RateLimiter
	var authenticator *auth.Authenticator
	var tenantInterceptor *auth.TenantInterceptor

	redisClient, err = redis.NewClient(redis.Config{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err != nil {
		if cfg.StartupMode.IsProduction() {
			return nil, nil, fmt.Errorf("redis required in production mode: %w", err)
		}
		slog.Warn("Redis not available - auth and rate limiting disabled (development mode)", "error", err)
	} else {
		keyStore = auth.NewKeyStore(redisClient)
		rateLimiter = auth.NewRateLimiter(redisClient, auth.RateLimits{
			RequestsPerMinute: cfg.RateLimits.DefaultRPM,
			RequestsPerDay:    cfg.RateLimits.DefaultRPD,
			TokensPerMinute:   cfg.RateLimits.DefaultTPM,
		}, true)
		authenticator = auth.NewAuthenticator(keyStore, rateLimiter)
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

	// Add auth interceptors if Redis is available
	if authenticator != nil {
		unaryInterceptors = append(unaryInterceptors, authenticator.UnaryInterceptor())
		streamInterceptors = append(streamInterceptors, authenticator.StreamInterceptor())
	} else if !cfg.StartupMode.IsProduction() {
		// Inject a dev client when auth is disabled in development mode
		unaryInterceptors = append(unaryInterceptors, developmentAuthInterceptor())
		streamInterceptors = append(streamInterceptors, developmentAuthStreamInterceptor())
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

	// Register services
	chatService := service.NewChatService(rateLimiter, ragService)
	pb.RegisterAIBoxServiceServer(server, chatService)

	adminService := service.NewAdminService(redisClient, service.AdminServiceConfig{
		Version:   version.Version,
		GitCommit: version.GitCommit,
		BuildTime: version.BuildTime,
		GoVersion: runtime.Version(),
	})
	pb.RegisterAdminServiceServer(server, adminService)

	// Register FileService if RAG is enabled
	if ragService != nil {
		fileService := service.NewFileService(ragService)
		pb.RegisterFileServiceServer(server, fileService)
	}

	tenantCount := 0
	if tenantMgr != nil {
		tenantCount = tenantMgr.TenantCount()
	}
	slog.Info("gRPC server created",
		"tls_enabled", cfg.TLS.Enabled,
		"auth_enabled", authenticator != nil,
		"multitenancy_enabled", tenantInterceptor != nil,
		"tenant_count", tenantCount,
		"version", version.Version,
	)

	components := &ServerComponents{
		KeyStore:    keyStore,
		RateLimiter: rateLimiter,
		TenantMgr:   tenantMgr,
		RedisClient: redisClient,
	}

	return server, components, nil
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
		if info.FullMethod != "/aibox.v1.AdminService/Health" {
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

// developmentAuthInterceptor injects a dev client in non-production mode when Redis is unavailable
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

// developmentAuthStreamInterceptor injects a dev client for streams in non-production mode
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
