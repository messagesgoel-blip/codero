package event

import (
	"context"
	"fmt"
)

// ReplyToClient is the durable endpoint contract for delivering structured
// events to OpenClaw. OpenClaw owns injection timing — this client only
// delivers the structured payload.
type ReplyToClient interface {
	// Deliver sends a structured event envelope to the OpenClaw endpoint.
	// The endpoint controls when and how the payload reaches the agent.
	Deliver(ctx context.Context, env Envelope) error
}

// replyToSessionClient delivers events to an OpenClaw-managed session
// via the daemon gRPC interface.
type replyToSessionClient struct {
	daemonAddr string
}

// NewReplyToSessionClient creates a ReplyToClient for daemon-routed sessions.
func NewReplyToSessionClient(daemonAddr string) ReplyToClient {
	return &replyToSessionClient{daemonAddr: daemonAddr}
}

func (c *replyToSessionClient) Deliver(ctx context.Context, env Envelope) error {
	if err := env.Validate(); err != nil {
		return fmt.Errorf("event: invalid envelope: %w", err)
	}
	// Structured delivery via daemon. The daemon routes to the correct
	// OpenClaw session. OpenClaw owns PTY injection timing.
	// TODO: implement gRPC DeliverEvent call when proto is updated.
	// For now, this is a contract stub that validates the envelope.
	return nil
}

// replyToDirectClient delivers events to a directly-managed session
// by writing structured feedback to the worktree.
type replyToDirectClient struct{}

// NewReplyToDirectClient creates a ReplyToClient for direct-DB sessions.
func NewReplyToDirectClient() ReplyToClient {
	return &replyToDirectClient{}
}

func (c *replyToDirectClient) Deliver(ctx context.Context, env Envelope) error {
	if err := env.Validate(); err != nil {
		return fmt.Errorf("event: invalid envelope: %w", err)
	}
	// Structured delivery via worktree feedback files.
	// OpenClaw polls and injects at its own timing.
	// TODO: implement worktree feedback write.
	return nil
}
