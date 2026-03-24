package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	"github.com/codero/codero/internal/state"
)

// feedbackService implements the FeedbackService gRPC service (Daemon Spec v2 §7.3).
type feedbackService struct {
	daemonv1.UnimplementedFeedbackServiceServer
	server *Server
}

// GetFeedback retrieves aggregated feedback for a task or assignment.
func (f *feedbackService) GetFeedback(ctx context.Context, req *daemonv1.GetFeedbackRequest) (*daemonv1.GetFeedbackResponse, error) {
	var fc *state.FeedbackCache
	var err error

	if req.AssignmentId != "" {
		// Assignment ID overrides task_id per spec.
		fc, err = state.GetFeedbackCacheByAssignment(ctx, f.server.db, req.AssignmentId)
	} else if req.TaskId != "" {
		fc, err = state.GetFeedbackCacheByTaskID(ctx, f.server.db, req.TaskId)
	} else {
		return nil, status.Error(codes.InvalidArgument, "task_id or assignment_id is required")
	}

	if err != nil {
		if errors.Is(err, state.ErrFeedbackCacheNotFound) {
			return &daemonv1.GetFeedbackResponse{
				TaskId: req.TaskId,
			}, nil
		}
		return nil, status.Errorf(codes.Internal, "get feedback: %v", err)
	}

	resp := &daemonv1.GetFeedbackResponse{
		TaskId:       fc.TaskID,
		ContextBlock: fc.ContextBlock,
	}

	// Map snapshot fields to feedback sources.
	if fc.CISnapshot != "" {
		resp.Sources = append(resp.Sources, &daemonv1.FeedbackSource{
			Source:   daemonv1.FeedbackSourceType_FEEDBACK_SOURCE_CI,
			Priority: 3,
		})
	}
	if fc.CoderabbitSnapshot != "" {
		resp.Sources = append(resp.Sources, &daemonv1.FeedbackSource{
			Source:   daemonv1.FeedbackSourceType_FEEDBACK_SOURCE_CODERABBIT,
			Priority: 2,
		})
	}
	if fc.HumanReviewSnapshot != "" {
		resp.Sources = append(resp.Sources, &daemonv1.FeedbackSource{
			Source:   daemonv1.FeedbackSourceType_FEEDBACK_SOURCE_HUMAN_REVIEW,
			Priority: 1,
		})
	}
	if fc.ComplianceSnapshot != "" {
		sourceStatus, suggestedSubstatus := feedbackStatusFromSourceStatus(fc.SourceStatus)
		resp.Sources = append(resp.Sources, &daemonv1.FeedbackSource{
			Source:   daemonv1.FeedbackSourceType_FEEDBACK_SOURCE_GATE,
			Priority: 3,
			Status:   sourceStatus,
		})
		resp.SuggestedSubstatus = suggestedSubstatus
	}

	if !fc.SnapshotAt.IsZero() {
		for _, src := range resp.Sources {
			src.LastUpdated = timestamppb.New(fc.SnapshotAt)
		}
	}

	if resp.SuggestedSubstatus == "" {
		_, resp.SuggestedSubstatus = feedbackStatusFromSourceStatus(fc.SourceStatus)
	}

	return resp, nil
}
