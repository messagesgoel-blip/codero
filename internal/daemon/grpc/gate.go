package grpc

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// gateService implements the GateService gRPC service (Daemon Spec v2 §7.4).
type gateService struct {
	daemonv1.UnimplementedGateServiceServer
	server *Server
}

// PostFindings records gate findings from the gate runner.
func (g *gateService) PostFindings(ctx context.Context, req *daemonv1.PostFindingsRequest) (*daemonv1.PostFindingsResponse, error) {
	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.AssignmentId == "" {
		return nil, status.Error(codes.InvalidArgument, "assignment_id is required")
	}
	if req.GateRunId == "" {
		return nil, status.Error(codes.InvalidArgument, "gate_run_id is required")
	}

	// Verify session/assignment ownership.
	assignment, err := state.GetAgentAssignmentByID(ctx, g.server.db, req.AssignmentId)
	if err != nil {
		if errors.Is(err, state.ErrAgentAssignmentNotFound) {
			return nil, status.Error(codes.NotFound, "assignment not found")
		}
		loglib.Error("grpc: PostFindings assignment lookup failed",
			loglib.FieldComponent, "grpc",
			"assignment_id", req.AssignmentId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to retrieve assignment")
	}
	if assignment.SessionID != req.SessionId {
		return nil, status.Error(codes.PermissionDenied, "assignment not owned by session")
	}

	overallStatus := ""
	switch req.OverallStatus {
	case daemonv1.GateOverallStatus_GATE_OVERALL_STATUS_PASS:
		overallStatus = "pass"
	case daemonv1.GateOverallStatus_GATE_OVERALL_STATUS_FAIL:
		overallStatus = "fail"
	case daemonv1.GateOverallStatus_GATE_OVERALL_STATUS_WARN:
		overallStatus = "warn"
	default:
		return nil, status.Error(codes.InvalidArgument, "overall_status is required")
	}

	existing, err := state.GetFeedbackCacheByAssignment(ctx, g.server.db, req.AssignmentId)
	if err != nil && !errors.Is(err, state.ErrFeedbackCacheNotFound) {
		loglib.Error("grpc: PostFindings cache lookup failed",
			loglib.FieldComponent, "grpc",
			"assignment_id", req.AssignmentId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to record findings")
	}
	if err == nil && existing.ComplianceSnapshot == req.GateRunId {
		return nil, status.Error(codes.AlreadyExists, "gate findings already recorded for assignment")
	}

	statuses := map[string]string{}
	if existing != nil {
		statuses = parseSourceStatuses(existing.SourceStatus)
	}
	statuses[sourceStatusCompliance] = overallStatus
	sourceStatus, err := marshalSourceStatuses(statuses)
	if err != nil {
		loglib.Error("grpc: PostFindings status marshal failed",
			loglib.FieldComponent, "grpc",
			"assignment_id", req.AssignmentId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to record findings")
	}

	// Persist gate findings to the feedback cache.
	fc := &state.FeedbackCache{
		CacheID:            uuid.New().String(),
		AssignmentID:       req.AssignmentId,
		SessionID:          req.SessionId,
		TaskID:             assignment.TaskID,
		ComplianceSnapshot: req.GateRunId,
		SourceStatus:       sourceStatus,
		SnapshotAt:         time.Now(),
	}
	if existing != nil {
		fc.CacheID = existing.CacheID
		fc.CISnapshot = existing.CISnapshot
		fc.CoderabbitSnapshot = existing.CoderabbitSnapshot
		fc.HumanReviewSnapshot = existing.HumanReviewSnapshot
		fc.ContextBlock = existing.ContextBlock
		fc.CacheHash = existing.CacheHash
	}
	if err := state.UpsertFeedbackCache(ctx, g.server.db, fc); err != nil {
		loglib.Error("grpc: PostFindings cache upsert failed",
			loglib.FieldComponent, "grpc",
			"assignment_id", req.AssignmentId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to record findings")
	}

	loglib.Info("grpc: gate findings recorded",
		loglib.FieldComponent, "grpc",
		"assignment_id", req.AssignmentId,
		"gate_run_id", req.GateRunId,
		"overall_status", overallStatus,
	)

	return &daemonv1.PostFindingsResponse{
		RecordedAt: timestamppb.New(time.Now()),
	}, nil
}
