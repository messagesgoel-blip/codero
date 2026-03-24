package grpc

import (
	"context"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
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

	if err := s.server.sessionStore.Register(ctx, sessionID, req.AgentId, clientKind); err != nil {
		loglib.Error("grpc: RegisterSession failed",
			loglib.FieldComponent, "grpc",
			"agent_id", req.AgentId,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "register session: %v", err)
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
func (s *sessionService) Heartbeat(ctx context.Context, req *daemonv1.HeartbeatRequest) (*daemonv1.HeartbeatResponse, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	if err := s.server.sessionStore.Heartbeat(ctx, req.SessionId, true); err != nil {
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
		return nil, status.Errorf(codes.NotFound, "session not found: %v", err)
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
