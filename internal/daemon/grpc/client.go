// Package grpc provides both the gRPC server implementation and a thin client
// for remote session management via the daemon's SessionService.
package grpc

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	"github.com/codero/codero/internal/session"
)

// SessionClient wraps the gRPC SessionService stub for use by CLI commands.
type SessionClient struct {
	conn   *grpc.ClientConn
	client daemonv1.SessionServiceClient
}

// NewSessionClient connects to the daemon at addr (e.g. "127.0.0.1:8110").
func NewSessionClient(addr string) (*SessionClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect to daemon at %s: %w", addr, err)
	}
	return &SessionClient{
		conn:   conn,
		client: daemonv1.NewSessionServiceClient(conn),
	}, nil
}

// Close releases the underlying connection.
func (c *SessionClient) Close() error {
	return c.conn.Close()
}

// RegisterResult holds the output of a Register call.
type RegisterResult struct {
	SessionID       string
	HeartbeatSecret string
}

// Register creates a new session and returns the session ID and heartbeat secret.
func (c *SessionClient) Register(ctx context.Context, agentID, clientKind string) (*RegisterResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Capture response headers to extract heartbeat secret (EL-23).
	var header metadata.MD
	resp, err := c.client.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    agentID,
		ClientKind: clientKind,
	}, grpc.Header(&header))
	if err != nil {
		return nil, fmt.Errorf("register session: %w", err)
	}

	secret := ""
	if vals := header.Get("x-heartbeat-secret"); len(vals) > 0 {
		secret = vals[0]
	}

	return &RegisterResult{
		SessionID:       resp.SessionId,
		HeartbeatSecret: secret,
	}, nil
}

// RegisterWithTmux creates a session with a client-provided session ID and tmux name,
// returning the heartbeat secret. Satisfies session.SessionBackend.
func (c *SessionClient) RegisterWithTmux(ctx context.Context, sessionID, agentID, mode, tmuxName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var header metadata.MD
	_, err := c.client.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:         agentID,
		ClientKind:      mode,
		SessionId:       sessionID,
		TmuxSessionName: tmuxName,
	}, grpc.Header(&header))
	if err != nil {
		return "", fmt.Errorf("register session: %w", err)
	}

	secret := ""
	if vals := header.Get("x-heartbeat-secret"); len(vals) > 0 {
		secret = vals[0]
	}
	return secret, nil
}

// Heartbeat proves a session is still alive. Requires the heartbeat secret from Register.
// When markProgress is true, the server also refreshes the session's progress_at timestamp.
func (c *SessionClient) Heartbeat(ctx context.Context, sessionID, heartbeatSecret string, markProgress bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ctx = metadata.AppendToOutgoingContext(ctx, "x-heartbeat-secret", heartbeatSecret)
	_, err := c.client.Heartbeat(ctx, &daemonv1.HeartbeatRequest{
		SessionId:    sessionID,
		MarkProgress: markProgress,
	})
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	return nil
}

// Confirm verifies the agent identity matches the registered session.
func (c *SessionClient) Confirm(ctx context.Context, sessionID, agentID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := c.client.ConfirmSession(ctx, &daemonv1.ConfirmSessionRequest{
		SessionId: sessionID,
		AgentId:   agentID,
	})
	if err != nil {
		return fmt.Errorf("confirm session: %w", err)
	}
	return nil
}

// AttachAssignment attaches a repo/branch to an active session.
func (c *SessionClient) AttachAssignment(
	ctx context.Context,
	sessionID, agentID, repo, branch, worktree, mode, taskID, substatus string,
) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := c.client.AttachAssignment(ctx, &daemonv1.AttachAssignmentRequest{
		SessionId: sessionID,
		AgentId:   agentID,
		Repo:      repo,
		Branch:    branch,
		Worktree:  worktree,
		Mode:      mode,
		TaskId:    taskID,
		Substatus: substatus,
	})
	if err != nil {
		return fmt.Errorf("attach assignment: %w", err)
	}
	return nil
}

// Finalize marks a session as complete.
func (c *SessionClient) Finalize(ctx context.Context, sessionID, agentID string, completion session.Completion) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req := &daemonv1.FinalizeSessionRequest{
		SessionId: sessionID,
		AgentId:   agentID,
		Status:    completion.Status,
		Substatus: completion.Substatus,
		Summary:   completion.Summary,
		TaskId:    completion.TaskID,
		Tests:     completion.Tests,
	}
	if !completion.FinishedAt.IsZero() {
		req.FinishedAt = timestamppb.New(completion.FinishedAt)
	}
	_, err := c.client.FinalizeSession(ctx, req)
	if err != nil {
		return fmt.Errorf("finalize session: %w", err)
	}
	return nil
}
