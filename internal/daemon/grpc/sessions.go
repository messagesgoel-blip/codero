package grpc

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// sessionService implements the SessionService gRPC service (Daemon Spec v2 §7.1).
type sessionService struct {
	daemonv1.UnimplementedSessionServiceServer
	server *Server
}

// RegisterSession registers a new agent session with the daemon.
func (s *sessionService) RegisterSession(ctx context.Context, req *daemonv1.RegisterSessionRequest) (*daemonv1.RegisterSessionResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	sessionID := uuid.New().String()
	clientKind := req.ClientKind
	if clientKind == "" {
		clientKind = "unknown"
	}

	secret, err := s.server.sessionStore.Register(ctx, sessionID, req.AgentId, clientKind)
	if err != nil {
		loglib.Error("grpc: RegisterSession failed",
			loglib.FieldComponent, "grpc",
			"agent_id", req.AgentId,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "register session: %v", err)
	}

	// EL-23: return heartbeat_secret via response trailer so only the launcher has it.
	trailer := metadata.Pairs("x-heartbeat-secret", secret)
	if err := grpclib.SendHeader(ctx, trailer); err != nil {
		loglib.Warn("grpc: failed to send heartbeat_secret header",
			loglib.FieldComponent, "grpc",
			"session_id", sessionID,
			"error", err,
		)
	}

	loglib.Info("grpc: session registered",
		loglib.FieldComponent, "grpc",
		"session_id", sessionID,
		"agent_id", req.AgentId,
		"client_kind", clientKind,
	)

	return &daemonv1.RegisterSessionResponse{
		SessionId:                sessionID,
		HeartbeatIntervalSeconds: 30,
		HeartbeatTtlSeconds:      120,
	}, nil
}

// Heartbeat proves a session is still alive.
// EL-23: requires x-heartbeat-secret metadata matching the value from RegisterSession.
func (s *sessionService) Heartbeat(ctx context.Context, req *daemonv1.HeartbeatRequest) (*daemonv1.HeartbeatResponse, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	// EL-23: extract heartbeat_secret from gRPC metadata.
	heartbeatSecret := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-heartbeat-secret"); len(vals) > 0 {
			heartbeatSecret = vals[0]
		}
	}

	if err := s.server.sessionStore.Heartbeat(ctx, req.SessionId, heartbeatSecret, true); err != nil {
		if errors.Is(err, state.ErrInvalidHeartbeatSecret) {
			return nil, status.Error(codes.PermissionDenied, "invalid heartbeat secret")
		}
		loglib.Warn("grpc: Heartbeat failed",
			loglib.FieldComponent, "grpc",
			"session_id", req.SessionId,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "heartbeat: %v", err)
	}

	return &daemonv1.HeartbeatResponse{
		Acknowledged: true,
		ServerTime:   timestamppb.New(time.Now()),
	}, nil
}

// GetSession retrieves session details.
func (s *sessionService) GetSession(ctx context.Context, req *daemonv1.GetSessionRequest) (*daemonv1.GetSessionResponse, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	sess, err := state.GetAgentSession(ctx, s.server.db, req.SessionId)
	if err != nil {
		if errors.Is(err, state.ErrAgentSessionNotFound) {
			return nil, status.Error(codes.NotFound, "session not found")
		}
		loglib.Error("grpc: GetSession failed",
			loglib.FieldComponent, "grpc",
			"session_id", req.SessionId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to get session")
	}

	resp := &daemonv1.GetSessionResponse{
		SessionId:  sess.SessionID,
		AgentId:    sess.AgentID,
		ClientKind: sess.Mode,
	}

	// Infer status from AgentSession fields: EndedAt presence + EndReason.
	if sess.EndedAt != nil {
		switch sess.EndReason {
		case "lost":
			resp.Status = daemonv1.SessionStatus_SESSION_STATUS_LOST
		case "expired":
			resp.Status = daemonv1.SessionStatus_SESSION_STATUS_EXPIRED
		default:
			resp.Status = daemonv1.SessionStatus_SESSION_STATUS_ENDED
		}
	} else {
		resp.Status = daemonv1.SessionStatus_SESSION_STATUS_ACTIVE
	}

	if !sess.StartedAt.IsZero() {
		resp.StartedAt = timestamppb.New(sess.StartedAt)
	}
	if !sess.LastSeenAt.IsZero() {
		resp.LastSeenAt = timestamppb.New(sess.LastSeenAt)
	}

	// Look up active assignment.
	assignment, err := state.GetActiveAgentAssignment(ctx, s.server.db, req.SessionId)
	if err != nil && !errors.Is(err, state.ErrAgentAssignmentNotFound) {
		loglib.Error("grpc: GetSession active assignment lookup failed",
			loglib.FieldComponent, "grpc",
			"session_id", req.SessionId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to get session")
	}
	if err == nil && assignment.ID != "" {
		resp.ActiveAssignment = &daemonv1.ActiveAssignmentSummary{
			AssignmentId: assignment.ID,
			TaskId:       assignment.TaskID,
			Repo:         assignment.Repo,
			Branch:       assignment.Branch,
			State:        assignment.State,
		}
	}

	return resp, nil
}
