package grpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
)

// sessionService implements the SessionService gRPC service (Daemon Spec v2 §7.1).
type sessionService struct {
	daemonv1.UnimplementedSessionServiceServer
	server *Server
}

// RegisterSession registers a new agent session with the daemon.
//
// Security model: Registration is unauthenticated by design — the daemon runs on
// loopback (127.0.0.1) and only trusted launchers can reach it. The ON CONFLICT
// upsert allows idempotent re-registration for launcher retries, but the
// heartbeat_secret is preserved from the original registration (not overwritten),
// so a subsequent caller cannot hijack heartbeats for an existing session (EL-23).
func (s *sessionService) RegisterSession(ctx context.Context, req *daemonv1.RegisterSessionRequest) (*daemonv1.RegisterSessionResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	sessionID := req.SessionId
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	clientKind := req.ClientKind
	if clientKind == "" {
		clientKind = "unknown"
	}

	var (
		secret string
		err    error
	)
	if req.TmuxSessionName != "" {
		secret, err = s.server.sessionStore.RegisterWithTmux(ctx, sessionID, req.AgentId, clientKind, req.TmuxSessionName)
	} else {
		secret, err = s.server.sessionStore.Register(ctx, sessionID, req.AgentId, clientKind)
	}
	if err != nil {
		loglib.Error("grpc: RegisterSession failed",
			loglib.FieldComponent, "grpc",
			"agent_id", req.AgentId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, fmt.Errorf("register session: %w", err).Error())
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

	if err := s.server.sessionStore.Heartbeat(ctx, req.SessionId, heartbeatSecret, req.MarkProgress); err != nil {
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

// ConfirmSession verifies that the agent identity matches the registered session.
func (s *sessionService) ConfirmSession(ctx context.Context, req *daemonv1.ConfirmSessionRequest) (*daemonv1.ConfirmSessionResponse, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	if err := s.server.sessionStore.Confirm(ctx, req.SessionId, req.AgentId); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return nil, status.Error(codes.NotFound, "session not found")
		}
		if errors.Is(err, session.ErrSessionMismatch) {
			return nil, status.Error(codes.PermissionDenied, "agent mismatch")
		}
		return nil, status.Errorf(codes.Internal, "confirm session: %v", err)
	}

	return &daemonv1.ConfirmSessionResponse{}, nil
}

// AttachAssignment attaches a repo/branch assignment to an active session.
func (s *sessionService) AttachAssignment(ctx context.Context, req *daemonv1.AttachAssignmentRequest) (*daemonv1.AttachAssignmentResponse, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	if req.Repo == "" || req.Branch == "" {
		return nil, status.Error(codes.InvalidArgument, "repo and branch are required")
	}

	if err := s.server.sessionStore.AttachAssignment(ctx,
		req.SessionId, req.AgentId,
		req.Repo, req.Branch, req.Worktree,
		req.Mode, req.TaskId, req.Substatus,
	); err != nil {
		if errors.Is(err, state.ErrBranchNotFound) {
			return nil, status.Error(codes.NotFound, "branch not found in state store")
		}
		loglib.Error("grpc: AttachAssignment failed",
			loglib.FieldComponent, "grpc",
			"session_id", req.SessionId,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "attach assignment: %v", err)
	}

	// NOTE: assignment_id is not populated yet — the session store's
	// AttachAssignment does not return the generated ID. Proto3 zero value
	// (empty string) is acceptable until the store API is extended.
	return &daemonv1.AttachAssignmentResponse{}, nil
}

// FinalizeSession marks a session as complete with a terminal status.
func (s *sessionService) FinalizeSession(ctx context.Context, req *daemonv1.FinalizeSessionRequest) (*daemonv1.FinalizeSessionResponse, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	if req.Status == "" {
		return nil, status.Error(codes.InvalidArgument, "status is required")
	}

	completion := session.Completion{
		TaskID:    req.TaskId,
		Status:    req.Status,
		Substatus: req.Substatus,
		Summary:   req.Summary,
		Tests:     req.Tests,
	}
	if req.FinishedAt != nil {
		completion.FinishedAt = req.FinishedAt.AsTime()
	}

	if err := s.server.sessionStore.Finalize(ctx, req.SessionId, req.AgentId, completion); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return nil, status.Error(codes.NotFound, "session not found or already ended")
		}
		if errors.Is(err, session.ErrSessionMismatch) {
			return nil, status.Error(codes.PermissionDenied, "agent mismatch")
		}
		loglib.Error("grpc: FinalizeSession failed",
			loglib.FieldComponent, "grpc",
			"session_id", req.SessionId,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "finalize session: %v", err)
	}

	return &daemonv1.FinalizeSessionResponse{
		FinalizedAt: timestamppb.New(time.Now()),
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
