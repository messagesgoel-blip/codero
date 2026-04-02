package session

import "context"

// SessionBackend abstracts session lifecycle operations so that agent launch
// can work against either a local Store (direct SQLite) or a remote gRPC daemon.
type SessionBackend interface {
	RegisterWithTmux(ctx context.Context, sessionID, agentID, mode, tmuxName string) (secret string, err error)
	Heartbeat(ctx context.Context, sessionID, heartbeatSecret string, markProgress bool) error
	HeartbeatWithStatus(ctx context.Context, sessionID, heartbeatSecret string, markProgress bool, inferredStatus string) error
	Confirm(ctx context.Context, sessionID, agentID string) error
	Get(ctx context.Context, sessionID, agentID string) (*SessionInfo, error)
	AttachAssignment(ctx context.Context, sessionID, agentID, repo, branch, worktree, mode, taskID, substatus string) error
	Finalize(ctx context.Context, sessionID, agentID string, completion Completion) error
}
