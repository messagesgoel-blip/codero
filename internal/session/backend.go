package session

import "context"

// SessionBackend abstracts session lifecycle operations so that agent launch
// can work against either a local Store (direct SQLite) or a remote gRPC daemon.
type SessionBackend interface {
	RegisterWithTmux(ctx context.Context, sessionID, agentID, mode, tmuxName string) (secret string, err error)
	Finalize(ctx context.Context, sessionID, agentID string, completion Completion) error
}
