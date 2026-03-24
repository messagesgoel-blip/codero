package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// assignmentService implements the AssignmentService gRPC service (Daemon Spec v2 §7.2, §7.6).
type assignmentService struct {
	daemonv1.UnimplementedAssignmentServiceServer
	server *Server
}

// GetAssignment retrieves assignment details.
func (a *assignmentService) GetAssignment(ctx context.Context, req *daemonv1.GetAssignmentRequest) (*daemonv1.GetAssignmentResponse, error) {
	if req.AssignmentId == "" {
		return nil, status.Error(codes.InvalidArgument, "assignment_id is required")
	}

	assignment, err := state.GetAgentAssignmentByID(ctx, a.server.db, req.AssignmentId)
	if err != nil {
		if errors.Is(err, state.ErrAgentAssignmentNotFound) {
			return nil, status.Error(codes.NotFound, "assignment not found")
		}
		loglib.Error("grpc: GetAssignment failed",
			loglib.FieldComponent, "grpc",
			"assignment_id", req.AssignmentId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to retrieve assignment")
	}

	resp := &daemonv1.GetAssignmentResponse{
		AssignmentId: assignment.ID,
		SessionId:    assignment.SessionID,
		TaskId:       assignment.TaskID,
		Substatus:    assignment.Substatus,
		Repo:         assignment.Repo,
		Branch:       assignment.Branch,
		Worktree:     assignment.Worktree,
	}

	switch assignment.State {
	case "active":
		resp.State = daemonv1.AssignmentState_ASSIGNMENT_STATE_ACTIVE
	case "blocked":
		resp.State = daemonv1.AssignmentState_ASSIGNMENT_STATE_BLOCKED
	case "completed":
		resp.State = daemonv1.AssignmentState_ASSIGNMENT_STATE_COMPLETED
	case "cancelled":
		resp.State = daemonv1.AssignmentState_ASSIGNMENT_STATE_CANCELLED
	case "superseded":
		resp.State = daemonv1.AssignmentState_ASSIGNMENT_STATE_SUPERSEDED
	case "lost":
		resp.State = daemonv1.AssignmentState_ASSIGNMENT_STATE_LOST
	}

	if !assignment.StartedAt.IsZero() {
		resp.CreatedAt = timestamppb.New(assignment.StartedAt)
	}

	return resp, nil
}

// Submit handles an agent submitting code for the delivery pipeline.
// Per spec §7.6: returns 409 if pipeline already running.
func (a *assignmentService) Submit(ctx context.Context, req *daemonv1.SubmitRequest) (*daemonv1.SubmitResponse, error) {
	if req.AssignmentId == "" {
		return nil, status.Error(codes.InvalidArgument, "assignment_id is required")
	}
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	// Verify assignment exists and belongs to the session.
	assignment, err := state.GetAgentAssignmentByID(ctx, a.server.db, req.AssignmentId)
	if err != nil {
		if errors.Is(err, state.ErrAgentAssignmentNotFound) {
			return nil, status.Error(codes.NotFound, "assignment not found")
		}
		loglib.Error("grpc: Submit assignment lookup failed",
			loglib.FieldComponent, "grpc",
			"assignment_id", req.AssignmentId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to retrieve assignment")
	}
	if assignment.SessionID != req.SessionId {
		return nil, status.Error(codes.PermissionDenied, "assignment not owned by session")
	}

	running, err := state.IsPipelineRunning(ctx, a.server.db, assignment.Repo, assignment.Branch)
	if err != nil {
		loglib.Error("grpc: Submit pipeline check failed",
			loglib.FieldComponent, "grpc",
			"assignment_id", req.AssignmentId,
			"repo", assignment.Repo,
			"branch", assignment.Branch,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to retrieve assignment")
	}
	if running {
		return nil, status.Error(codes.AlreadyExists, "pipeline already running for assignment")
	}

	loglib.Info("grpc: submit received",
		loglib.FieldComponent, "grpc",
		"assignment_id", req.AssignmentId,
		"session_id", req.SessionId,
		"summary", req.Summary,
	)

	// The delivery pipeline is wired separately; this endpoint accepts the submit
	// signal and acknowledges it. The actual pipeline execution is async.
	return &daemonv1.SubmitResponse{
		PipelineId:            req.AssignmentId + "-pipeline",
		EstimatedStartSeconds: 0,
	}, nil
}
