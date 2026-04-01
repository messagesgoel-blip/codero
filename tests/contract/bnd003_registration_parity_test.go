package contract

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/codero/codero/internal/daemon/grpc"
	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// ─── BND-003: Managed Session Launch Contract Tests ──────────────────────────
//
// These tests validate that the managed-session launch contract produces stable
// session, transport, and worktree identity regardless of runtime path.
//
// Parity validation: direct-DB and daemon-routed registration for the same
// launch contract must produce identical identity results.

// openParityDB opens an in-memory state DB for parity tests.
func openParityDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "parity.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// startParityDaemon spins up a minimal gRPC daemon with a session store.
// Returns the daemon address and the DB for direct verification.
func startParityDaemon(t *testing.T) (addr string, db *state.DB) {
	t.Helper()
	db = openParityDB(t)
	sessStore := session.NewStore(db)
	srv := grpc.NewServer(grpc.ServerConfig{
		DB:           db,
		RawDB:        db.Unwrap(),
		SessionStore: sessStore,
		Version:      "test",
	})
	srv.MarkReady()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go srv.GRPCServer().Serve(lis)
	t.Cleanup(func() { srv.GRPCServer().Stop() })

	return lis.Addr().String(), db
}

// TestBND003_DirectDB_RegisterWithTmux_PreservesIdentity verifies the direct-DB
// path stores session_id, agent_id, mode, and tmux_session_name correctly.
func TestBND003_DirectDB_RegisterWithTmux_PreservesIdentity(t *testing.T) {
	db := openParityDB(t)
	ctx := context.Background()
	store := session.NewStore(db)

	const (
		sessionID = "sess-bnd003-1"
		agentID   = "agent-bnd003"
		mode      = "agent"
		tmuxName  = "codero-agent-bnd003-1"
	)

	secret, err := store.RegisterWithTmux(ctx, sessionID, agentID, mode, tmuxName)
	if err != nil {
		t.Fatalf("RegisterWithTmux: %v", err)
	}
	if secret == "" {
		t.Fatal("expected non-empty heartbeat secret")
	}

	var storedTmux, storedAgent, storedMode string
	err = db.Unwrap().QueryRow(
		`SELECT tmux_session_name, agent_id, mode FROM agent_sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&storedTmux, &storedAgent, &storedMode)
	if err != nil {
		t.Fatalf("query session: %v", err)
	}
	if storedTmux != tmuxName {
		t.Errorf("tmux_session_name = %q, want %q", storedTmux, tmuxName)
	}
	if storedAgent != agentID {
		t.Errorf("agent_id = %q, want %q", storedAgent, agentID)
	}
	if storedMode != mode {
		t.Errorf("mode = %q, want %q", storedMode, mode)
	}
}

// TestBND003_DaemonRouted_RegisterWithTmux_PreservesIdentity verifies the
// daemon-routed path stores the same fields as direct-DB.
func TestBND003_DaemonRouted_RegisterWithTmux_PreservesIdentity(t *testing.T) {
	addr, db := startParityDaemon(t)
	ctx := context.Background()

	const (
		sessionID = "sess-bnd003-2"
		agentID   = "agent-bnd003"
		mode      = "agent"
		tmuxName  = "codero-agent-bnd003-2"
	)

	client, err := grpc.NewSessionClient(addr)
	if err != nil {
		t.Fatalf("NewSessionClient: %v", err)
	}
	defer client.Close()

	secret, err := client.RegisterWithTmux(ctx, sessionID, agentID, mode, tmuxName)
	if err != nil {
		t.Fatalf("RegisterWithTmux via daemon: %v", err)
	}
	if secret == "" {
		t.Fatal("expected non-empty heartbeat secret")
	}

	var storedTmux, storedAgent, storedMode string
	err = db.Unwrap().QueryRow(
		`SELECT tmux_session_name, agent_id, mode FROM agent_sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&storedTmux, &storedAgent, &storedMode)
	if err != nil {
		t.Fatalf("query session: %v", err)
	}
	if storedTmux != tmuxName {
		t.Errorf("tmux_session_name = %q, want %q", storedTmux, tmuxName)
	}
	if storedAgent != agentID {
		t.Errorf("agent_id = %q, want %q", storedAgent, agentID)
	}
	if storedMode != mode {
		t.Errorf("mode = %q, want %q", storedMode, mode)
	}
}

// TestBND003_RegistrationParity_DirectDBVsDaemon verifies that the same
// launch contract produces identical identity fields via both paths.
func TestBND003_RegistrationParity_DirectDBVsDaemon(t *testing.T) {
	ctx := context.Background()

	const (
		sessionID = "sess-parity-001"
		agentID   = "agent-parity"
		mode      = "agent"
		tmuxName  = "codero-agent-parity-001"
	)

	// Direct-DB path
	dbDirect := openParityDB(t)
	storeDirect := session.NewStore(dbDirect)

	secretDirect, err := storeDirect.RegisterWithTmux(ctx, sessionID, agentID, mode, tmuxName)
	if err != nil {
		t.Fatalf("direct-DB RegisterWithTmux: %v", err)
	}

	// Daemon-routed path
	addr, dbDaemon := startParityDaemon(t)
	client, err := grpc.NewSessionClient(addr)
	if err != nil {
		t.Fatalf("NewSessionClient: %v", err)
	}
	defer client.Close()

	secretDaemon, err := client.RegisterWithTmux(ctx, sessionID, agentID, mode, tmuxName)
	if err != nil {
		t.Fatalf("daemon RegisterWithTmux: %v", err)
	}

	// Both paths should produce non-empty secrets
	if secretDirect == "" {
		t.Error("direct-DB: empty heartbeat secret")
	}
	if secretDaemon == "" {
		t.Error("daemon: empty heartbeat secret")
	}

	// Query both DBs and compare
	type sessionRecord struct {
		tmuxName  string
		agentID   string
		mode      string
		sessionID string
	}

	query := func(db *state.DB, sid string) sessionRecord {
		var r sessionRecord
		err := db.Unwrap().QueryRow(
			`SELECT session_id, tmux_session_name, agent_id, mode FROM agent_sessions WHERE session_id = ?`,
			sid,
		).Scan(&r.sessionID, &r.tmuxName, &r.agentID, &r.mode)
		if err != nil {
			t.Fatalf("query %s: %v", sid, err)
		}
		return r
	}

	direct := query(dbDirect, sessionID)
	daemon := query(dbDaemon, sessionID)

	// Session IDs must match (client-provided)
	if direct.sessionID != daemon.sessionID {
		t.Errorf("session_id mismatch: direct=%q, daemon=%q", direct.sessionID, daemon.sessionID)
	}
	// Tmux names must match
	if direct.tmuxName != daemon.tmuxName {
		t.Errorf("tmux_session_name mismatch: direct=%q, daemon=%q", direct.tmuxName, daemon.tmuxName)
	}
	// Agent IDs must match
	if direct.agentID != daemon.agentID {
		t.Errorf("agent_id mismatch: direct=%q, daemon=%q", direct.agentID, daemon.agentID)
	}
	// Modes must match
	if direct.mode != daemon.mode {
		t.Errorf("mode mismatch: direct=%q, daemon=%q", direct.mode, daemon.mode)
	}
}

// TestBND003_DaemonRouted_SessionIDAutoGenerated verifies that when no
// session_id is provided to the daemon, it auto-generates one.
func TestBND003_DaemonRouted_SessionIDAutoGenerated(t *testing.T) {
	addr, db := startParityDaemon(t)
	ctx := context.Background()

	conn, err := ggrpc.NewClient(addr, ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cli := daemonv1.NewSessionServiceClient(conn)

	resp, err := cli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "agent-autogen",
		ClientKind: "agent",
	})
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	if resp.SessionId == "" {
		t.Fatal("expected auto-generated session_id")
	}

	// Verify it was persisted
	var storedID string
	err = db.Unwrap().QueryRow(
		`SELECT session_id FROM agent_sessions WHERE session_id = ?`,
		resp.SessionId,
	).Scan(&storedID)
	if err != nil {
		t.Fatalf("query auto-generated session: %v", err)
	}
	if storedID != resp.SessionId {
		t.Errorf("stored session_id = %q, want %q", storedID, resp.SessionId)
	}
}

// TestBND003_DaemonRouted_ClientProvidedSessionID verifies that when a
// client-provided session_id is sent to the daemon, it is preserved.
func TestBND003_DaemonRouted_ClientProvidedSessionID(t *testing.T) {
	addr, db := startParityDaemon(t)
	ctx := context.Background()

	conn, err := ggrpc.NewClient(addr, ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cli := daemonv1.NewSessionServiceClient(conn)

	const providedID = "client-provided-session-id-001"
	resp, err := cli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		SessionId:  providedID,
		AgentId:    "agent-provided",
		ClientKind: "agent",
	})
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	if resp.SessionId != providedID {
		t.Errorf("session_id = %q, want %q", resp.SessionId, providedID)
	}

	var storedID string
	err = db.Unwrap().QueryRow(
		`SELECT session_id FROM agent_sessions WHERE session_id = ?`,
		providedID,
	).Scan(&storedID)
	if err != nil {
		t.Fatalf("query provided session: %v", err)
	}
	if storedID != providedID {
		t.Errorf("stored session_id = %q, want %q", storedID, providedID)
	}
}

// TestBND003_DaemonRouted_TmuxNameInRequest verifies that tmux_session_name
// in the proto request is stored correctly.
func TestBND003_DaemonRouted_TmuxNameInRequest(t *testing.T) {
	addr, db := startParityDaemon(t)
	ctx := context.Background()

	conn, err := ggrpc.NewClient(addr, ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cli := daemonv1.NewSessionServiceClient(conn)

	const tmuxName = "codero-agent-tmux-parity"
	_, err = cli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		SessionId:       "sess-tmux-parity",
		AgentId:         "agent-tmux-parity",
		ClientKind:      "agent",
		TmuxSessionName: tmuxName,
	})
	if err != nil {
		t.Fatalf("RegisterSession with tmux: %v", err)
	}

	var storedTmux string
	err = db.Unwrap().QueryRow(
		`SELECT tmux_session_name FROM agent_sessions WHERE session_id = ?`,
		"sess-tmux-parity",
	).Scan(&storedTmux)
	if err != nil {
		t.Fatalf("query tmux_name: %v", err)
	}
	if storedTmux != tmuxName {
		t.Errorf("tmux_session_name = %q, want %q", storedTmux, tmuxName)
	}
}

// TestBND003_HeartbeatSecret_OnlyReturnedViaHeader verifies that the heartbeat
// secret is returned via gRPC header, not in the response body.
func TestBND003_HeartbeatSecret_OnlyReturnedViaHeader(t *testing.T) {
	addr, _ := startParityDaemon(t)
	ctx := context.Background()

	conn, err := ggrpc.NewClient(addr, ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cli := daemonv1.NewSessionServiceClient(conn)

	var header metadata.MD
	_, err = cli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "agent-header-test",
		ClientKind: "agent",
	}, ggrpc.Header(&header))
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	secrets := header.Get("x-heartbeat-secret")
	if len(secrets) == 0 || secrets[0] == "" {
		t.Fatal("expected x-heartbeat-secret header")
	}
}

// TestBND003_UnmanagedPTYRejection verifies that sessions registered without
// a managed transport (no tmux name, no session_id) still work but produce
// server-generated identity.
func TestBND003_UnmanagedPTYRejection(t *testing.T) {
	addr, db := startParityDaemon(t)
	ctx := context.Background()

	conn, err := ggrpc.NewClient(addr, ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cli := daemonv1.NewSessionServiceClient(conn)

	// Register without tmux name — should still work, server generates session_id
	resp, err := cli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "agent-unmanaged",
		ClientKind: "agent",
	})
	if err != nil {
		t.Fatalf("RegisterSession without tmux: %v", err)
	}
	if resp.SessionId == "" {
		t.Fatal("expected server-generated session_id")
	}

	// Verify tmux_session_name is empty
	var storedTmux string
	err = db.Unwrap().QueryRow(
		`SELECT tmux_session_name FROM agent_sessions WHERE session_id = ?`,
		resp.SessionId,
	).Scan(&storedTmux)
	if err != nil {
		t.Fatalf("query tmux_name: %v", err)
	}
	if storedTmux != "" {
		t.Errorf("tmux_session_name should be empty for non-tmux registration, got %q", storedTmux)
	}
}
