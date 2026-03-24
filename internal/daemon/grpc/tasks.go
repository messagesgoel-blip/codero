package grpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	loglib "github.com/codero/codero/internal/log"
)

// taskService implements the TaskService gRPC service (Daemon Spec v2 §7.2).
type taskService struct {
	daemonv1.UnimplementedTaskServiceServer
	server *Server
}

// IngestTask ingests a new task from an external orchestrator.
// Tasks are recorded as pending branch_states entries awaiting assignment dispatch.
func (t *taskService) IngestTask(ctx context.Context, req *daemonv1.IngestTaskRequest) (*daemonv1.IngestTaskResponse, error) {
	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.Repo == "" {
		return nil, status.Error(codes.InvalidArgument, "repo is required")
	}
	if req.Title == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}

	branch := req.Branch
	if branch == "" {
		branch = fmt.Sprintf("codero/%s", req.TaskId)
	}

	// Record task as a queued branch_state entry.
	_, err := t.server.rawDB.ExecContext(ctx, `
		INSERT OR IGNORE INTO branch_states (repo, branch, state, updated_at)
		VALUES (?, ?, 'queued', datetime('now'))`,
		req.Repo, branch)
	if err != nil {
		loglib.Error("grpc: IngestTask failed",
			loglib.FieldComponent, "grpc",
			"task_id", req.TaskId,
			"error", err,
		)
		return nil, status.Errorf(codes.Internal, "ingest task: %v", err)
	}

	loglib.Info("grpc: task ingested",
		loglib.FieldComponent, "grpc",
		"task_id", req.TaskId,
		"repo", req.Repo,
		"branch", branch,
	)

	return &daemonv1.IngestTaskResponse{
		TaskId:     req.TaskId,
		EnqueuedAt: timestamppb.Now(),
	}, nil
}
