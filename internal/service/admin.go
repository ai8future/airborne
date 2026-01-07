package service

import (
	"context"
	"time"

	pb "github.com/cliffpyles/aibox/gen/go/aibox/v1"
	"github.com/cliffpyles/aibox/internal/auth"
	"github.com/cliffpyles/aibox/internal/redis"
)

// AdminService implements the AdminService gRPC service.
type AdminService struct {
	pb.UnimplementedAdminServiceServer

	redis     *redis.Client
	version   string
	gitCommit string
	buildTime string
	goVersion string
	startTime time.Time
}

// AdminServiceConfig contains admin service configuration.
type AdminServiceConfig struct {
	Version   string
	GitCommit string
	BuildTime string
	GoVersion string
}

// NewAdminService creates a new admin service.
func NewAdminService(redisClient *redis.Client, cfg AdminServiceConfig) *AdminService {
	return &AdminService{
		redis:     redisClient,
		version:   cfg.Version,
		gitCommit: cfg.GitCommit,
		buildTime: cfg.BuildTime,
		goVersion: cfg.GoVersion,
		startTime: time.Now(),
	}
}

// Health returns basic health status.
func (s *AdminService) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	uptime := int64(time.Since(s.startTime).Seconds())

	return &pb.HealthResponse{
		Status:        "healthy",
		Version:       s.version,
		UptimeSeconds: uptime,
	}, nil
}

// Ready returns readiness status with dependency checks.
func (s *AdminService) Ready(ctx context.Context, req *pb.ReadyRequest) (*pb.ReadyResponse, error) {
	// Check permission - Ready exposes internal state
	if err := auth.RequirePermission(ctx, auth.PermissionAdmin); err != nil {
		return nil, err
	}

	dependencies := make(map[string]*pb.DependencyStatus)

	// Check Redis
	redisStatus := &pb.DependencyStatus{Healthy: true}
	if s.redis != nil {
		start := time.Now()
		if err := s.redis.Ping(ctx); err != nil {
			redisStatus.Healthy = false
			redisStatus.Message = err.Error()
		} else {
			redisStatus.LatencyMs = time.Since(start).Milliseconds()
		}
	} else {
		redisStatus.Healthy = false
		redisStatus.Message = "not configured"
	}
	dependencies["redis"] = redisStatus

	// Determine overall readiness
	ready := true
	for _, dep := range dependencies {
		if !dep.Healthy {
			ready = false
			break
		}
	}

	return &pb.ReadyResponse{
		Ready:        ready,
		Dependencies: dependencies,
	}, nil
}

// Version returns detailed version information.
func (s *AdminService) Version(ctx context.Context, req *pb.VersionRequest) (*pb.VersionResponse, error) {
	// Check permission - Version exposes build details
	if err := auth.RequirePermission(ctx, auth.PermissionAdmin); err != nil {
		return nil, err
	}

	return &pb.VersionResponse{
		Version:   s.version,
		GitCommit: s.gitCommit,
		BuildTime: s.buildTime,
		GoVersion: s.goVersion,
	}, nil
}
