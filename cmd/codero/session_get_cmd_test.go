package main

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"

	daemongrpc "github.com/codero/codero/internal/daemon/grpc"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
)

func startSessionGetTestDaemon(t *testing.T) (*session.Store, string) {
	t.Helper()

	db, err := state.Open(filepath.Join(t.TempDir(), "daemon.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := session.NewStore(db)
	srv := daemongrpc.NewServer(daemongrpc.ServerConfig{
		DB:           db,
		RawDB:        db.Unwrap(),
		SessionStore: store,
		Version:      "test",
	})
	srv.MarkReady()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		srv.GRPCServer().Stop()
		_ = lis.Close()
	})
	go srv.GRPCServer().Serve(lis)

	return store, lis.Addr().String()
}

func TestSessionGetCmd_UsesDaemonWhenConfigured(t *testing.T) {
	store, daemonAddr := startSessionGetTestDaemon(t)
	ctx := context.Background()

	if _, err := store.Register(ctx, "daemon-sess-1", "daemon-agent", "cli"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	out, err := runCmd(t, sessionCmd,
		"--daemon-addr", daemonAddr,
		"get",
		"--session", "daemon-sess-1",
		"--agent-id", "daemon-agent",
		"--json",
	)
	if err != nil {
		t.Fatalf("session get via daemon: %v\noutput: %s", err, out)
	}

	var info session.SessionInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("json.Unmarshal: %v\noutput: %s", err, out)
	}
	if info.Session.SessionID != "daemon-sess-1" {
		t.Fatalf("session_id: got %q, want daemon-sess-1", info.Session.SessionID)
	}
	if info.Session.AgentID != "daemon-agent" {
		t.Fatalf("agent_id: got %q, want daemon-agent", info.Session.AgentID)
	}
	if info.Session.Mode != "cli" {
		t.Fatalf("mode: got %q, want cli", info.Session.Mode)
	}
	if info.Session.InferredStatus != "active" {
		t.Fatalf("status: got %q, want active", info.Session.InferredStatus)
	}
}
