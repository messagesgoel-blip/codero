// Package grpc implements the Daemon Spec v2 gRPC surface.
// It provides a gRPC server that implements Sessions, Tasks, Assignments,
// Feedback, Gate, and Health services as defined in the protobuf contract.
//
// The server integrates with the daemon lifecycle via readiness gating:
// all RPCs return UNAVAILABLE until MarkReady is called after the full
// daemon bootstrap completes (spec §3, step 12).
package grpc

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server wraps a gRPC server and all daemon service implementations.
// It provides lifecycle control (Start/Stop) and readiness gating.
type Server struct {
	grpcServer *ggrpc.Server
	ready      atomic.Bool
	startTime  time.Time
	version    string

	// Dependencies injected at construction.
	db           *state.DB
	githubHealth GitHubHealthSource
	rawDB        *sql.DB
	sessionStore *session.Store

	// Session recovery service for continuity across restarts
	sessionRecovery *SessionRecoveryService
}

// GitHubHealthSource reports the latest reconciler GitHub probe state.
type GitHubHealthSource interface {
	GitHubProbeStatus() (checkedAt time.Time, healthy bool, errText string, ok bool)
}

// ServerConfig holds configuration for the gRPC server.
type ServerConfig struct {
	DB           *state.DB
	GitHubHealth GitHubHealthSource
	RawDB        *sql.DB
	SessionStore *session.Store
	Version      string
}

// NewServer creates a gRPC server with all daemon services registered.
// The server starts in not-ready state; call MarkReady after bootstrap.
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		startTime:    time.Now(),
		version:      cfg.Version,
		db:           cfg.DB,
		githubHealth: cfg.GitHubHealth,
		rawDB:        cfg.RawDB,
		sessionStore: cfg.SessionStore,
	}

	// Initialize session recovery service
	s.sessionRecovery = NewSessionRecoveryService(cfg.RawDB, slog.Default())

	s.grpcServer = ggrpc.NewServer(
		ggrpc.UnaryInterceptor(s.readinessInterceptor),
	)

	// Register all daemon services.
	daemonv1.RegisterSessionServiceServer(s.grpcServer, &sessionService{server: s})
	daemonv1.RegisterTaskServiceServer(s.grpcServer, &taskService{server: s})
	daemonv1.RegisterAssignmentServiceServer(s.grpcServer, &assignmentService{server: s})
	daemonv1.RegisterFeedbackServiceServer(s.grpcServer, &feedbackService{server: s})
	daemonv1.RegisterGateServiceServer(s.grpcServer, &gateService{server: s})
	daemonv1.RegisterHealthServiceServer(s.grpcServer, &healthService{server: s})

	return s
}

// RecoverSessionsAfterRestart performs session continuity recovery after daemon restart
func (s *Server) RecoverSessionsAfterRestart(ctx context.Context) error {
	return s.sessionRecovery.RecoverActiveSessions(ctx)
}

// IsSessionRecoverable checks if a session can be recovered after restart
func (s *Server) IsSessionRecoverable(ctx context.Context, sessionID, agentID string) (bool, error) {
	return s.sessionRecovery.IsSessionRecoverable(ctx, sessionID, agentID)
}

// Serve starts the gRPC server on the given listener. Blocks until stopped.
func (s *Server) Serve(lis net.Listener) error {
	loglib.Info("codero: gRPC server starting",
		loglib.FieldEventType, "grpc_start",
		loglib.FieldComponent, "daemon",
		"addr", lis.Addr().String(),
	)
	return s.grpcServer.Serve(lis)
}

// GracefulStop initiates a graceful shutdown of the gRPC server.
func (s *Server) GracefulStop() {
	s.grpcServer.GracefulStop()
}

// Stop immediately stops the gRPC server.
func (s *Server) Stop() {
	s.grpcServer.Stop()
}

// MarkReady signals that the daemon bootstrap is complete.
// RPCs return UNAVAILABLE until this is called.
func (s *Server) MarkReady() {
	s.ready.Store(true)
}

// MarkNotReady clears readiness, used during shutdown.
func (s *Server) MarkNotReady() {
	s.ready.Store(false)
}

// IsReady reports whether the server is accepting RPCs.
func (s *Server) IsReady() bool {
	return s.ready.Load()
}

// GRPCServer returns the underlying gRPC server for integration with cmux/h2c.
func (s *Server) GRPCServer() *ggrpc.Server {
	return s.grpcServer
}

// readinessInterceptor rejects all RPCs with UNAVAILABLE until MarkReady is called.
// Per Daemon Spec v2 §3: "Server returns 503 on all routes until startup recovery
// sweep completes (readiness gate)."
func (s *Server) readinessInterceptor(
	ctx context.Context,
	req interface{},
	info *ggrpc.UnaryServerInfo,
	handler ggrpc.UnaryHandler,
) (interface{}, error) {
	if !s.ready.Load() {
		return nil, status.Error(codes.Unavailable, "daemon not ready")
	}
	return handler(ctx, req)
}
