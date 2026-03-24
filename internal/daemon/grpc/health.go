package grpc

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/codero/codero/internal/daemon"
	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
)

// healthService implements the HealthService gRPC service (Daemon Spec v2 §7.4, §7.5).
type healthService struct {
	daemonv1.UnimplementedHealthServiceServer
	server *Server
}

// GetHealth returns the overall daemon health status.
func (h *healthService) GetHealth(ctx context.Context, req *daemonv1.GetHealthRequest) (*daemonv1.GetHealthResponse, error) {
	st := "ok"
	redisStatus := "ok"

	if daemon.IsDegraded() {
		st = "degraded"
		redisStatus = "unavailable"
	}

	return &daemonv1.GetHealthResponse{
		Status:        st,
		UptimeSeconds: time.Since(h.server.startTime).Seconds(),
		Version:       h.server.version,
		Ready:         h.server.ready.Load(),
		RedisStatus:   redisStatus,
	}, nil
}

// GetGitHubStatus returns the GitHub connectivity status.
func (h *healthService) GetGitHubStatus(ctx context.Context, req *daemonv1.GetGitHubStatusRequest) (*daemonv1.GetGitHubStatusResponse, error) {
	if h.server.githubHealth == nil {
		return &daemonv1.GetGitHubStatusResponse{
			Status: daemonv1.GitHubAvailability_GITHUB_AVAILABILITY_UNAVAILABLE,
		}, nil
	}

	lastCheck, healthy, errText, ok := h.server.githubHealth.GitHubProbeStatus()
	if !ok || !healthy {
		return &daemonv1.GetGitHubStatusResponse{
			Status: daemonv1.GitHubAvailability_GITHUB_AVAILABILITY_UNAVAILABLE,
			Error:  errText,
		}, nil
	}

	resp := &daemonv1.GetGitHubStatusResponse{
		Status: daemonv1.GitHubAvailability_GITHUB_AVAILABILITY_HEALTHY,
	}
	if !lastCheck.IsZero() {
		resp.LastCheck = timestamppb.New(lastCheck)
	}
	return resp, nil
}
