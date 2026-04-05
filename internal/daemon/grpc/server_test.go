package grpc

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/codero/codero/internal/daemon"
	daemonv1 "github.com/codero/codero/internal/daemon/grpc/v1"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
	"github.com/google/uuid"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func seedTrackedBranch(t *testing.T, db *state.DB, repo, branch string) {
	t.Helper()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states
			(id, repo, branch, head_hash, state, retry_count, max_retries, approved, ci_green,
			 pending_events, unresolved_threads, owner_session_id, queue_priority)
		VALUES (?,?,?,?,?,0,3,0,0,0,0,'',0)`,
		repo+"-"+branch, repo, branch, "abc123", "submitted",
	)
	if err != nil {
		t.Fatalf("seedTrackedBranch: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *state.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := state.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testServer spins up a fully-wired gRPC daemon server on a random port and
// returns clients for every service. The server is already marked ready.
func testServer(t *testing.T) (
	*Server,
	daemonv1.SessionServiceClient,
	daemonv1.HealthServiceClient,
	daemonv1.TaskServiceClient,
	daemonv1.AssignmentServiceClient,
	daemonv1.FeedbackServiceClient,
	daemonv1.GateServiceClient,
) {
	t.Helper()
	db := openTestDB(t)
	sessStore := session.NewStore(db)
	srv := NewServer(ServerConfig{
		DB:           db,
		RawDB:        db.Unwrap(),
		SessionStore: sessStore,
		Version:      "test-0.0.1",
	})
	srv.MarkReady()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go srv.GRPCServer().Serve(lis)
	t.Cleanup(func() { srv.GRPCServer().Stop() })

	conn, err := ggrpc.NewClient(
		lis.Addr().String(),
		ggrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return srv,
		daemonv1.NewSessionServiceClient(conn),
		daemonv1.NewHealthServiceClient(conn),
		daemonv1.NewTaskServiceClient(conn),
		daemonv1.NewAssignmentServiceClient(conn),
		daemonv1.NewFeedbackServiceClient(conn),
		daemonv1.NewGateServiceClient(conn)
}

// testServerNotReady is the same as testServer but does NOT call MarkReady.
func testServerNotReady(t *testing.T) (
	*Server,
	daemonv1.SessionServiceClient,
) {
	t.Helper()
	db := openTestDB(t)
	sessStore := session.NewStore(db)
	srv := NewServer(ServerConfig{
		DB:           db,
		RawDB:        db.Unwrap(),
		SessionStore: sessStore,
		Version:      "test-0.0.1",
	})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go srv.GRPCServer().Serve(lis)
	t.Cleanup(func() { srv.GRPCServer().Stop() })

	conn, err := ggrpc.NewClient(
		lis.Addr().String(),
		ggrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return srv, daemonv1.NewSessionServiceClient(conn)
}

func requireCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %v, got nil", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != want {
		t.Fatalf("expected code %v, got %v (msg: %s)", want, st.Code(), st.Message())
	}
}

type fakeGitHubHealth struct {
	checkedAt time.Time
	healthy   bool
	errText   string
	ok        bool
}

func (f fakeGitHubHealth) GitHubProbeStatus() (time.Time, bool, string, bool) {
	return f.checkedAt, f.healthy, f.errText, f.ok
}

// ---------------------------------------------------------------------------
// 1. Readiness Gate Tests
// ---------------------------------------------------------------------------

func TestReadinessInterceptor_NotReady(t *testing.T) {
	_, sessCli := testServerNotReady(t)
	ctx := context.Background()

	_, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{AgentId: "a1"})
	requireCode(t, err, codes.Unavailable)
}

func TestReadinessInterceptor_AfterMarkReady(t *testing.T) {
	srv, sessCli := testServerNotReady(t)
	ctx := context.Background()

	srv.MarkReady()

	resp, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{AgentId: "a1"})
	if err != nil {
		t.Fatalf("expected success after MarkReady, got %v", err)
	}
	if resp.SessionId == "" {
		t.Fatal("expected non-empty session_id")
	}
}

func TestReadinessInterceptor_AfterMarkNotReady(t *testing.T) {
	srv, sessCli := testServerNotReady(t)
	ctx := context.Background()

	srv.MarkReady()

	// Verify RPC works.
	_, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{AgentId: "a1"})
	if err != nil {
		t.Fatalf("RPC should succeed while ready: %v", err)
	}

	srv.MarkNotReady()

	_, err = sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{AgentId: "a2"})
	requireCode(t, err, codes.Unavailable)
}

// ---------------------------------------------------------------------------
// 2. Session Service Tests
// ---------------------------------------------------------------------------

func TestRegisterSession(t *testing.T) {
	_, sessCli, _, _, _, _, _ := testServer(t)
	ctx := context.Background()

	resp, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "agent-42",
		ClientKind: "cli",
	})
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	if resp.SessionId == "" {
		t.Fatal("session_id must not be empty")
	}
	if resp.HeartbeatIntervalSeconds <= 0 {
		t.Fatalf("heartbeat_interval_seconds should be >0, got %d", resp.HeartbeatIntervalSeconds)
	}
	if resp.HeartbeatTtlSeconds <= 0 {
		t.Fatalf("heartbeat_ttl_seconds should be >0, got %d", resp.HeartbeatTtlSeconds)
	}
}

func TestRegisterSession_MissingAgentID(t *testing.T) {
	_, sessCli, _, _, _, _, _ := testServer(t)
	ctx := context.Background()

	_, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{})
	requireCode(t, err, codes.InvalidArgument)
}

func TestHeartbeat(t *testing.T) {
	_, sessCli, _, _, _, _, _ := testServer(t)
	ctx := context.Background()

	var header metadata.MD
	reg, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{AgentId: "hb-agent"}, ggrpc.Header(&header))
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	secrets := header.Get("x-heartbeat-secret")
	if len(secrets) == 0 || secrets[0] == "" {
		t.Fatal("expected heartbeat secret header")
	}

	hbCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("x-heartbeat-secret", secrets[0]))
	hb, err := sessCli.Heartbeat(hbCtx, &daemonv1.HeartbeatRequest{SessionId: reg.SessionId})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if !hb.Acknowledged {
		t.Fatal("expected acknowledged=true")
	}
	if hb.ServerTime == nil {
		t.Fatal("expected non-nil server_time")
	}
}

func TestRegisterSession_PersistsLaunchContextAndPromotesTrackedAssignment(t *testing.T) {
	srv, sessCli, _, _, _, _, _ := testServer(t)
	ctx := context.Background()

	seedTrackedBranch(t, srv.db, "acme/api", "feat/COD-201-runtime")

	var header metadata.MD
	resp, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "codex-a",
		ClientKind: "coding",
		InitialContext: map[string]string{
			"cwd":            "/workspace/acme-api",
			"repo":           "acme/api",
			"branch":         "feat/COD-201-runtime",
			"session_source": "agent_run",
			"task_id":        "COD-201",
		},
	}, ggrpc.Header(&header))
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	var repo, branch, source, confidence string
	if err := srv.db.Unwrap().QueryRowContext(ctx, `
		SELECT repo, branch, attribution_source, attribution_confidence
		FROM agent_sessions
		WHERE session_id = ?`, resp.SessionId).
		Scan(&repo, &branch, &source, &confidence); err != nil {
		t.Fatalf("read session runtime attribution: %v", err)
	}
	if repo != "acme/api" || branch != "feat/COD-201-runtime" {
		t.Fatalf("repo/branch = %q/%q, want launch context values", repo, branch)
	}
	if source != state.AttributionSourceLaunchContext || confidence != state.AttributionConfidenceMedium {
		t.Fatalf("attribution = %q/%q, want launch_context/medium", source, confidence)
	}

	active, err := state.GetActiveAgentAssignment(ctx, srv.db, resp.SessionId)
	if err != nil {
		t.Fatalf("GetActiveAgentAssignment: %v", err)
	}
	if active.Repo != "acme/api" || active.Branch != "feat/COD-201-runtime" {
		t.Fatalf("active assignment repo/branch = %q/%q, want tracked promotion", active.Repo, active.Branch)
	}
	if active.TaskID != "COD-201" {
		t.Fatalf("active assignment task_id = %q, want COD-201", active.TaskID)
	}
}

func TestHeartbeat_PromotesLightweightAssignmentFromHookMetadata(t *testing.T) {
	srv, sessCli, _, _, _, _, _ := testServer(t)
	ctx := context.Background()

	seedTrackedBranch(t, srv.db, "acme/api", "feat/COD-202-hook")

	var header metadata.MD
	resp, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "gcli-a",
		ClientKind: "coding",
		InitialContext: map[string]string{
			"cwd": "/workspace/home",
		},
	}, ggrpc.Header(&header))
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	secrets := header.Get("x-heartbeat-secret")
	if len(secrets) == 0 || secrets[0] == "" {
		t.Fatal("expected heartbeat secret header")
	}

	hbCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"x-heartbeat-secret", secrets[0],
		"x-repo", "acme/api",
		"x-branch", "feat/COD-202-hook",
		"x-attribution-source", state.AttributionSourceHookMetadata,
	))
	if _, err := sessCli.Heartbeat(hbCtx, &daemonv1.HeartbeatRequest{SessionId: resp.SessionId, MarkProgress: true}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	active, err := state.GetActiveAgentAssignment(ctx, srv.db, resp.SessionId)
	if err != nil {
		t.Fatalf("GetActiveAgentAssignment: %v", err)
	}
	if active.Repo != "acme/api" || active.Branch != "feat/COD-202-hook" {
		t.Fatalf("active assignment repo/branch = %q/%q, want promoted tracked branch", active.Repo, active.Branch)
	}

	var source, confidence string
	if err := srv.db.Unwrap().QueryRowContext(ctx, `
		SELECT attribution_source, attribution_confidence
		FROM agent_sessions
		WHERE session_id = ?`, resp.SessionId).
		Scan(&source, &confidence); err != nil {
		t.Fatalf("read session attribution: %v", err)
	}
	if source != state.AttributionSourceHookMetadata || confidence != state.AttributionConfidenceHigh {
		t.Fatalf("attribution = %q/%q, want hook_metadata/high", source, confidence)
	}
}

func TestHeartbeat_InvalidAttributionSourceFallsBackToExplicitHeartbeat(t *testing.T) {
	srv, sessCli, _, _, _, _, _ := testServer(t)
	ctx := context.Background()

	var header metadata.MD
	resp, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "codex-a",
		ClientKind: "coding",
	}, ggrpc.Header(&header))
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	secrets := header.Get("x-heartbeat-secret")
	if len(secrets) == 0 || secrets[0] == "" {
		t.Fatal("expected heartbeat secret header")
	}

	hbCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"x-heartbeat-secret", secrets[0],
		"x-repo", "acme/api",
		"x-branch", "feat/COD-203-invalid-source",
		"x-attribution-source", "not-a-real-source",
	))
	if _, err := sessCli.Heartbeat(hbCtx, &daemonv1.HeartbeatRequest{SessionId: resp.SessionId}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	var source, confidence string
	if err := srv.db.Unwrap().QueryRowContext(ctx, `
		SELECT attribution_source, attribution_confidence
		FROM agent_sessions
		WHERE session_id = ?`, resp.SessionId).
		Scan(&source, &confidence); err != nil {
		t.Fatalf("read session attribution: %v", err)
	}
	if source != state.AttributionSourceExplicitHeartbeat || confidence != state.AttributionConfidenceHigh {
		t.Fatalf("attribution = %q/%q, want explicit_heartbeat/high", source, confidence)
	}
}

func TestGetSession(t *testing.T) {
	_, sessCli, _, _, _, _, _ := testServer(t)
	ctx := context.Background()

	reg, err := sessCli.RegisterSession(ctx, &daemonv1.RegisterSessionRequest{
		AgentId:    "get-agent",
		ClientKind: "vscode",
	})
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	got, err := sessCli.GetSession(ctx, &daemonv1.GetSessionRequest{SessionId: reg.SessionId})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.SessionId != reg.SessionId {
		t.Fatalf("session_id mismatch: want %s, got %s", reg.SessionId, got.SessionId)
	}
	if got.AgentId != "get-agent" {
		t.Fatalf("agent_id mismatch: want get-agent, got %s", got.AgentId)
	}
	if got.Status != daemonv1.SessionStatus_SESSION_STATUS_ACTIVE {
		t.Fatalf("expected ACTIVE status, got %v", got.Status)
	}
}

// ---------------------------------------------------------------------------
// 3. Health Service Tests
// ---------------------------------------------------------------------------

func TestGetHealth(t *testing.T) {
	_, _, healthCli, _, _, _, _ := testServer(t)
	ctx := context.Background()

	resp, err := healthCli.GetHealth(ctx, &daemonv1.GetHealthRequest{})
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", resp.Status)
	}
	if resp.Version != "test-0.0.1" {
		t.Fatalf("expected version test-0.0.1, got %s", resp.Version)
	}
	if resp.UptimeSeconds <= 0 {
		t.Fatalf("expected positive uptime, got %f", resp.UptimeSeconds)
	}
	if !resp.Ready {
		t.Fatal("expected ready=true")
	}
}

func TestGetHealth_DegradedMode(t *testing.T) {
	_, _, healthCli, _, _, _, _ := testServer(t)
	ctx := context.Background()

	daemon.SetDegraded(true)
	t.Cleanup(func() { daemon.SetDegraded(false) })

	resp, err := healthCli.GetHealth(ctx, &daemonv1.GetHealthRequest{})
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if resp.Status != "degraded" {
		t.Fatalf("expected status degraded, got %s", resp.Status)
	}
	if resp.RedisStatus != "unavailable" {
		t.Fatalf("expected redis_status unavailable, got %s", resp.RedisStatus)
	}
}

func TestGetGitHubStatus_NoProbeReturnsUnavailable(t *testing.T) {
	_, _, healthCli, _, _, _, _ := testServer(t)
	ctx := context.Background()

	resp, err := healthCli.GetGitHubStatus(ctx, &daemonv1.GetGitHubStatusRequest{})
	if err != nil {
		t.Fatalf("GetGitHubStatus: %v", err)
	}
	if resp.Status != daemonv1.GitHubAvailability_GITHUB_AVAILABILITY_UNAVAILABLE {
		t.Fatalf("expected unavailable, got %v", resp.Status)
	}
	if resp.LastCheck != nil {
		t.Fatalf("expected nil last_check before first probe, got %v", resp.LastCheck)
	}
}

func TestGetGitHubStatus_ReportsProbeResult(t *testing.T) {
	db := openTestDB(t)
	sessStore := session.NewStore(db)
	srv := NewServer(ServerConfig{
		DB:           db,
		RawDB:        db.Unwrap(),
		GitHubHealth: fakeGitHubHealth{checkedAt: time.Unix(1711281600, 0).UTC(), healthy: true, ok: true},
		SessionStore: sessStore,
		Version:      "test-0.0.1",
	})
	srv.MarkReady()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go srv.GRPCServer().Serve(lis)
	t.Cleanup(func() { srv.GRPCServer().Stop() })

	conn, err := ggrpc.NewClient(
		lis.Addr().String(),
		ggrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	healthCli := daemonv1.NewHealthServiceClient(conn)
	resp, err := healthCli.GetGitHubStatus(context.Background(), &daemonv1.GetGitHubStatusRequest{})
	if err != nil {
		t.Fatalf("GetGitHubStatus: %v", err)
	}
	if resp.Status != daemonv1.GitHubAvailability_GITHUB_AVAILABILITY_HEALTHY {
		t.Fatalf("expected healthy, got %v", resp.Status)
	}
	if resp.LastCheck == nil {
		t.Fatal("expected non-nil last_check")
	}
}

// ---------------------------------------------------------------------------
// 4. Task Service Tests
// ---------------------------------------------------------------------------

func TestIngestTask(t *testing.T) {
	_, _, _, taskCli, _, _, _ := testServer(t)
	ctx := context.Background()

	resp, err := taskCli.IngestTask(ctx, &daemonv1.IngestTaskRequest{
		TaskId:      "task-001",
		Repo:        "codero/codero",
		Title:       "Fix flaky test",
		Description: "The TestFoo test is flaky under race",
	})
	if err != nil {
		t.Fatalf("IngestTask: %v", err)
	}
	if resp.TaskId != "task-001" {
		t.Fatalf("task_id mismatch: want task-001, got %s", resp.TaskId)
	}
	if resp.EnqueuedAt == nil {
		t.Fatal("expected non-nil enqueued_at")
	}
}

func TestIngestTask_MissingFields(t *testing.T) {
	_, _, _, taskCli, _, _, _ := testServer(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  *daemonv1.IngestTaskRequest
	}{
		{"missing task_id", &daemonv1.IngestTaskRequest{Repo: "r", Title: "t"}},
		{"missing repo", &daemonv1.IngestTaskRequest{TaskId: "id", Title: "t"}},
		{"missing title", &daemonv1.IngestTaskRequest{TaskId: "id", Repo: "r"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := taskCli.IngestTask(ctx, tc.req)
			requireCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestIngestTask_ConflictingTaskIDForBranch(t *testing.T) {
	_, _, _, taskCli, _, _, _ := testServer(t)
	ctx := context.Background()

	req := &daemonv1.IngestTaskRequest{
		TaskId: "task-001",
		Repo:   "codero/codero",
		Branch: "codero/shared",
		Title:  "First task",
	}
	if _, err := taskCli.IngestTask(ctx, req); err != nil {
		t.Fatalf("first IngestTask: %v", err)
	}
	if _, err := taskCli.IngestTask(ctx, req); err != nil {
		t.Fatalf("idempotent IngestTask: %v", err)
	}

	_, err := taskCli.IngestTask(ctx, &daemonv1.IngestTaskRequest{
		TaskId: "task-002",
		Repo:   "codero/codero",
		Branch: "codero/shared",
		Title:  "Second task",
	})
	requireCode(t, err, codes.AlreadyExists)
}

// ---------------------------------------------------------------------------
// 5. Assignment + Gate + Feedback integration
// ---------------------------------------------------------------------------

func TestGetAssignment_NotFound(t *testing.T) {
	_, _, _, _, assignCli, _, _ := testServer(t)
	ctx := context.Background()

	_, err := assignCli.GetAssignment(ctx, &daemonv1.GetAssignmentRequest{
		AssignmentId: "nonexistent-assignment-id",
	})
	requireCode(t, err, codes.NotFound)
}

func TestSubmit_MissingFields(t *testing.T) {
	_, _, _, _, assignCli, _, _ := testServer(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  *daemonv1.SubmitRequest
	}{
		{"missing assignment_id", &daemonv1.SubmitRequest{SessionId: "s1"}},
		{"missing session_id", &daemonv1.SubmitRequest{AssignmentId: "a1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := assignCli.Submit(ctx, tc.req)
			requireCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestSubmit_DuplicateRunningPipeline(t *testing.T) {
	srv, _, _, _, assignCli, _, _ := testServer(t)
	ctx := context.Background()

	if err := state.RegisterAgentSession(ctx, srv.db, "sess-submit", "agent-submit", "", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	assignment := &state.AgentAssignment{
		ID:        "assign-submit",
		SessionID: "sess-submit",
		AgentID:   "agent-submit",
		Repo:      "codero/codero",
		Branch:    "feat/duplicate-submit",
		Worktree:  filepath.Join(t.TempDir(), "duplicate-submit"),
		TaskID:    "TASK-SUBMIT",
		State:     "active",
		Substatus: state.AssignmentSubstatusInProgress,
	}
	if err := state.AttachAgentAssignment(ctx, srv.db, assignment); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}
	startedAt := time.Now().UTC()
	if err := state.CreateReviewRun(srv.db, &state.ReviewRun{
		ID:        uuid.NewString(),
		Repo:      assignment.Repo,
		Branch:    assignment.Branch,
		Provider:  "stub",
		Status:    "running",
		StartedAt: &startedAt,
	}); err != nil {
		t.Fatalf("CreateReviewRun: %v", err)
	}

	_, err := assignCli.Submit(ctx, &daemonv1.SubmitRequest{
		AssignmentId: assignment.ID,
		SessionId:    assignment.SessionID,
		Summary:      "ready",
	})
	requireCode(t, err, codes.AlreadyExists)
}

func TestGetFeedback_MissingFields(t *testing.T) {
	_, _, _, _, _, fbCli, _ := testServer(t)
	ctx := context.Background()

	_, err := fbCli.GetFeedback(ctx, &daemonv1.GetFeedbackRequest{})
	requireCode(t, err, codes.InvalidArgument)
}

func TestPostFindings_MissingFields(t *testing.T) {
	_, _, _, _, _, _, gateCli := testServer(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  *daemonv1.PostFindingsRequest
	}{
		{"missing session_id", &daemonv1.PostFindingsRequest{AssignmentId: "a1", GateRunId: "gr1"}},
		{"missing assignment_id", &daemonv1.PostFindingsRequest{SessionId: "s1", GateRunId: "gr1"}},
		{"missing gate_run_id", &daemonv1.PostFindingsRequest{SessionId: "s1", AssignmentId: "a1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gateCli.PostFindings(ctx, tc.req)
			requireCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestPostFindings_SuccessAndDuplicate(t *testing.T) {
	srv, _, _, _, _, fbCli, gateCli := testServer(t)
	ctx := context.Background()

	if err := state.RegisterAgentSession(ctx, srv.db, "sess-gate", "agent-gate", "", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	assignment := &state.AgentAssignment{
		ID:        "assign-gate",
		SessionID: "sess-gate",
		AgentID:   "agent-gate",
		TaskID:    "TASK-GATE",
		State:     "active",
		Substatus: state.AssignmentSubstatusInProgress,
	}
	if err := state.AttachAgentAssignment(ctx, srv.db, assignment); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}

	req := &daemonv1.PostFindingsRequest{
		SessionId:     assignment.SessionID,
		AssignmentId:  assignment.ID,
		GateRunId:     "gate-run-1",
		OverallStatus: daemonv1.GateOverallStatus_GATE_OVERALL_STATUS_FAIL,
	}
	if _, err := gateCli.PostFindings(ctx, req); err != nil {
		t.Fatalf("PostFindings: %v", err)
	}

	resp, err := fbCli.GetFeedback(ctx, &daemonv1.GetFeedbackRequest{
		AssignmentId: assignment.ID,
	})
	if err != nil {
		t.Fatalf("GetFeedback: %v", err)
	}
	if resp.TaskId != assignment.TaskID {
		t.Fatalf("task_id mismatch: got %q want %q", resp.TaskId, assignment.TaskID)
	}
	if resp.SuggestedSubstatus != "needs_revision" {
		t.Fatalf("suggested_substatus: got %q want needs_revision", resp.SuggestedSubstatus)
	}
	if len(resp.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(resp.Sources))
	}
	if resp.Sources[0].Source != daemonv1.FeedbackSourceType_FEEDBACK_SOURCE_GATE {
		t.Fatalf("source: got %v want gate", resp.Sources[0].Source)
	}
	if resp.Sources[0].Status != daemonv1.FeedbackStatus_FEEDBACK_STATUS_ACTIONABLE {
		t.Fatalf("status: got %v want actionable", resp.Sources[0].Status)
	}

	_, err = gateCli.PostFindings(ctx, req)
	requireCode(t, err, codes.AlreadyExists)
}
