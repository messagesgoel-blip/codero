package grpc

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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
	res, err := t.server.rawDB.ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, task_id, state, updated_at)
		VALUES (?, ?, ?, ?, 'submitted', datetime('now'))
		ON CONFLICT(repo, branch) DO NOTHING`,
		uuid.NewString(), req.Repo, branch, req.TaskId)
	if err != nil {
		loglib.Error("grpc: IngestTask failed",
			loglib.FieldComponent, "grpc",
			"task_id", req.TaskId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to ingest task")
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		loglib.Error("grpc: IngestTask rows affected failed",
			loglib.FieldComponent, "grpc",
			"task_id", req.TaskId,
			"error", err,
		)
		return nil, status.Error(codes.Internal, "failed to ingest task")
	}
	if rowsAffected == 0 {
		var existingTaskID string
		if err := t.server.rawDB.QueryRowContext(ctx, `
			SELECT COALESCE(task_id, '')
			FROM branch_states
			WHERE repo = ? AND branch = ?`,
			req.Repo, branch,
		).Scan(&existingTaskID); err != nil {
			loglib.Error("grpc: IngestTask existing branch lookup failed",
				loglib.FieldComponent, "grpc",
				"task_id", req.TaskId,
				"repo", req.Repo,
				"branch", branch,
				"error", err,
			)
			return nil, status.Error(codes.Internal, "failed to ingest task")
		}
		if existingTaskID != req.TaskId {
			return nil, status.Error(codes.AlreadyExists, "branch already tracked for a different task")
		}
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
