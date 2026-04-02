package grpc

import (
	"context"
	"errors"
	"testing"

	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	"github.com/codero/codero/internal/session"
)

func TestRegisterWithSession_RequiresSessionID(t *testing.T) {
	client := &SessionClient{}

	if _, err := client.RegisterWithSession(context.Background(), "", "agent", "agent"); err == nil {
		t.Fatal("RegisterWithSession should reject an empty sessionID")
	}
}

func TestSessionClientGet(t *testing.T) {
	_, sessCli, _, _, _, _, _ := testServer(t)
	client := &SessionClient{client: sessCli}
	ctx := context.Background()

	reg, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "get-agent",
		ClientKind: "cli",
	})
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	info, err := client.Get(ctx, reg.SessionId, "get-agent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if info.Session.SessionID != reg.SessionId {
		t.Fatalf("session_id: got %q, want %q", info.Session.SessionID, reg.SessionId)
	}
	if info.Session.AgentID != "get-agent" {
		t.Fatalf("agent_id: got %q, want get-agent", info.Session.AgentID)
	}
	if info.Session.Mode != "cli" {
		t.Fatalf("mode: got %q, want cli", info.Session.Mode)
	}
	if info.Session.InferredStatus != "active" {
		t.Fatalf("status: got %q, want active", info.Session.InferredStatus)
	}
}

func TestSessionClientGet_RejectsAgentMismatch(t *testing.T) {
	_, sessCli, _, _, _, _, _ := testServer(t)
	client := &SessionClient{client: sessCli}
	ctx := context.Background()

	reg, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "get-agent",
		ClientKind: "cli",
	})
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	_, err = client.Get(ctx, reg.SessionId, "other-agent")
	if !errors.Is(err, session.ErrSessionMismatch) {
		t.Fatalf("Get mismatch: got %v, want %v", err, session.ErrSessionMismatch)
	}
}
