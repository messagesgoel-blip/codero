package event

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
// by writing structured feedback to the worktree or injecting via PTY bridge.
type replyToDirectClient struct{}

const defaultPTYBridgePath = "/srv/storage/shared/tools/bin/agent-tmux-bridge"

var execPTYBridgeCommand = func(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, defaultPTYBridgePath, args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	return cmd.CombinedOutput()
}

// NewReplyToDirectClient creates a ReplyToClient for direct-DB sessions.
// It uses the shared PTY bridge at /srv/storage/shared/tools/bin/agent-tmux-bridge.
func NewReplyToDirectClient() ReplyToClient {
	return &replyToDirectClient{}
}

var (
	// ErrNoRoutingInfo is returned when an envelope is missing TmuxName or AgentKind
	// needed for PTY bridge delivery.
	ErrNoRoutingInfo = errors.New("event: missing routing info (TmuxName/AgentKind) for bridge delivery")
)

func (c *replyToDirectClient) Deliver(ctx context.Context, env Envelope) error {
	if err := env.Validate(); err != nil {
		return fmt.Errorf("event: invalid envelope: %w", err)
	}

	// BND-004: Codero emits structured payloads only. If we are using the PTY
	// bridge, we are acting as the transport layer (OpenClaw).
	if env.ReplyTo.TmuxName != "" && env.ReplyTo.AgentKind != "" {
		return c.deliverViaBridge(ctx, env)
	}

	if env.ReplyTo.TmuxName == "" || env.ReplyTo.AgentKind == "" {
		return fmt.Errorf("%w (SessionID=%s)", ErrNoRoutingInfo, env.ReplyTo.SessionID)
	}

	// Fallback/Legacy: Structured delivery via worktree feedback files.
	// OpenClaw polls and injects at its own timing.
	// TODO: implement worktree feedback write if bridge is not applicable.
	return nil
}

func (c *replyToDirectClient) deliverViaBridge(ctx context.Context, env Envelope) error {
	msg, err := formatPayloadForPTY(env)
	if err != nil {
		return fmt.Errorf("event: format payload: %w", err)
	}

	args := []string{
		"deliver",
		"--session", env.ReplyTo.TmuxName,
		"--profile", env.ReplyTo.AgentKind,
		"--message-stdin",
	}

	if out, err := execPTYBridgeCommand(ctx, []byte(msg), args...); err != nil {
		return fmt.Errorf("event: bridge delivery failed: %w (output: %s)", err, string(out))
	}

	return nil
}

func formatPayloadForPTY(env Envelope) (string, error) {
	switch env.Type {
	case EventTypeFeedbackInject:
		var p FeedbackInjectPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return "", err
		}
		var sb strings.Builder
		sb.WriteString("Codero has provided feedback on your last delivery.\n")
		if p.OperatorNote != "" {
			fmt.Fprintf(&sb, "Operator Note: %s\n", p.OperatorNote)
		}
		if len(p.Findings) > 0 {
			sb.WriteString("\nFindings:\n")
			for _, f := range p.Findings {
				if f.File != "" {
					if f.Line > 0 {
						fmt.Fprintf(&sb, "- %s:%d: %s\n", f.File, f.Line, f.Message)
						continue
					}
					fmt.Fprintf(&sb, "- %s: %s\n", f.File, f.Message)
					continue
				}
				if f.Message != "" {
					fmt.Fprintf(&sb, "- %s\n", f.Message)
				}
			}
		}
		return sb.String(), nil
	default:
		// Generic fallback for other event types if they need PTY injection.
		return string(env.Payload), nil
	}
}
