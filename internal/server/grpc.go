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

// NewGRPCServer creates a new gRPC server with all services registered
func NewGRPCServer(cfg *config.Config, version VersionInfo) (*grpc.Server, error) {
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
			return nil, fmt.Errorf("redis required in production mode: %w", err)
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
			return nil, err
		}
		opts = append(opts, grpc.Creds(creds))
	}

	// Create server
	server := grpc.NewServer(opts...)

	// Register services
	chatService := service.NewChatService(rateLimiter)
	pb.RegisterAIBoxServiceServer(server, chatService)

	adminService := service.NewAdminService(redisClient, service.AdminServiceConfig{
		Version:   version.Version,
		GitCommit: version.GitCommit,
		BuildTime: version.BuildTime,
		GoVersion: runtime.Version(),
	})
	pb.RegisterAdminServiceServer(server, adminService)

	// TODO: Register FileService

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

	return server, nil
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
