package dashboard

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/codero/codero/internal/state"
)

func openChatOpenAITestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db.Unwrap()
}

func TestAppendConversationTurn_AppendsUserAndAssistant(t *testing.T) {
	h := &Handler{convos: NewConversationStore(10, 3600)}

	h.appendConversationTurn("conv-1", "hello", "hi there")

	history := h.chatHistoryForConversation("conv-1")
	if len(history) != 2 {
		t.Fatalf("history len=%d, want 2", len(history))
	}
	if history[0].Role != "user" || history[0].Content != "hello" {
		t.Fatalf("first history message = %+v, want user hello", history[0])
	}
	if history[1].Role != "assistant" || history[1].Content != "hi there" {
		t.Fatalf("second history message = %+v, want assistant hi there", history[1])
	}
}

func TestAssembleChatContextMarkdown_ScopesAndBudget(t *testing.T) {
	t.Setenv("CODERO_CHAT_MAX_CONTEXT_SIZE", "2000")
	db := openChatOpenAITestDB(t)

	h := NewHandler(db, NewSettingsStore(t.TempDir()))
	now := time.Now().UTC()

	for i := 1; i <= 5; i++ {
		sessionID := strings.ToLower(strings.TrimSpace("sess-" + string(rune('0'+i))))
		agentID := "agent-" + string(rune('a'+i-1))
		branch := "feat-" + string(rune('a'+i-1))
		repo := "acme/api"
		substatus := "waiting_for_merge_approval"
		if i%2 == 0 {
			repo = "acme/web"
			substatus = "waiting_for_ci"
		}
		_, err := db.Exec(`INSERT INTO agent_sessions
			(session_id, agent_id, mode, started_at, last_seen_at, ended_at, end_reason)
			VALUES (?,?,?,?,?,NULL,'')`,
			sessionID, agentID, "cli", now.Add(-time.Duration(i)*time.Minute), now.Add(-time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("seed agent session: %v", err)
		}
		_, err = db.Exec(`INSERT INTO agent_assignments
			(assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus, started_at, ended_at, end_reason, superseded_by)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,NULL,'',NULL)`,
			"assign-"+string(rune('0'+i)), sessionID, agentID, repo, branch, "worktree-"+branch, "task-"+string(rune('0'+i)), "active", "", substatus, now.Add(-time.Duration(i)*2*time.Minute))
		if err != nil {
			t.Fatalf("seed agent assignment: %v", err)
		}
	}

	for i := 1; i <= 2; i++ {
		repo := "acme/api"
		branch := "main"
		if i == 2 {
			repo = "acme/web"
			branch = "feat/web"
		}
		_, err := db.Exec(`INSERT INTO branch_states
			(id, repo, branch, head_hash, state, retry_count, max_retries, approved, ci_green,
			 pending_events, unresolved_threads, owner_session_id, owner_session_last_seen,
			 queue_priority, submission_time, created_at, updated_at)
			VALUES (?,?,?,?,?,0,3,0,0,0,0,?,?,?,datetime('now'),?,?)`,
			"branch-"+branch, repo, branch, "abc123", "queued_cli", "sess-1", now, i, now, now)
		if err != nil {
			t.Fatalf("seed branch state: %v", err)
		}
	}

	_, err := db.Exec(`INSERT INTO review_runs
		(id, repo, branch, head_hash, provider, status, started_at, finished_at, error, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		"run-1", "acme/api", "main", "abc123", "litellm", "completed", now.Add(-time.Hour), now.Add(-30*time.Minute), "", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("seed review run: %v", err)
	}

	for i := 1; i <= 2; i++ {
		_, err := db.Exec(`INSERT INTO session_archives
			(archive_id, session_id, agent_id, result, repo, branch, started_at, ended_at, duration_seconds, commit_count, archived_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			"arc-"+string(rune('0'+i)), "sess-1", "agent-a", "completed", "acme/api", "main", now.Add(-time.Hour), now.Add(-30*time.Minute), 180, i, now)
		if err != nil {
			t.Fatalf("seed archive: %v", err)
		}
		_, err = db.Exec(`INSERT INTO findings
			(id, run_id, repo, branch, severity, category, file, line, message, source, rule_id, ts)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			"find-"+string(rune('0'+i)), "run-1", "acme/api", "main", "error", "review", "file.go", i, "feedback message "+string(rune('0'+i)), "semgrep", "rule-1", now.Add(-time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("seed finding: %v", err)
		}
	}

	sessionsOnly := h.assembleChatContextMarkdown(context.Background(), "sessions")
	if !strings.Contains(sessionsOnly, "Active Sessions") {
		t.Fatalf("sessions context missing section header: %s", sessionsOnly)
	}
	if strings.Contains(sessionsOnly, "Queue") || strings.Contains(sessionsOnly, "Recent Archives") {
		t.Fatalf("sessions context leaked other sections: %s", sessionsOnly)
	}
	for i := 1; i <= 5; i++ {
		sessionID := "sess-" + string(rune('0'+i))
		if !strings.Contains(sessionsOnly, sessionID) {
			t.Fatalf("sessions context missing %s: %s", sessionID, sessionsOnly)
		}
	}

	all := h.assembleChatContextMarkdown(context.Background(), "all")
	if len(all) > 2000 {
		t.Fatalf("all-scope context len=%d exceeds budget: %s", len(all), all)
	}
	if !strings.Contains(all, "Active Sessions") {
		t.Fatalf("all-scope context missing active sessions: %s", all)
	}
}
