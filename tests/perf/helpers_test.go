package perf

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/state"
)

func openTestDB(tb testing.TB) *state.DB {
	tb.Helper()

	path := filepath.Join(tb.TempDir(), "perf.db")
	db, err := state.Open(path)
	if err != nil {
		tb.Fatalf("state.Open: %v", err)
	}
	tb.Cleanup(func() { _ = db.Close() })
	return db
}

func newRedisClient(tb testing.TB) *redislib.Client {
	tb.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		tb.Fatalf("miniredis: %v", err)
	}
	tb.Cleanup(mr.Close)

	client := redislib.New(mr.Addr(), "")
	tb.Cleanup(func() { client.Close() })

	return client
}

func percentileDuration(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	if math.IsNaN(percentile) || math.IsInf(percentile, 0) || percentile <= 0 || percentile > 1 {
		panic(fmt.Sprintf("percentileDuration: invalid percentile %v (must be in (0,1])", percentile))
	}

	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	index := int(math.Ceil(float64(len(sorted))*percentile)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func seedBranch(tb testing.TB, db *state.DB, id, repo, branch string) {
	tb.Helper()

	if _, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, state)
		VALUES (?, ?, ?, ?)`,
		id, repo, branch, string(state.StateSubmitted),
	); err != nil {
		tb.Fatalf("seed branch %s/%s: %v", repo, branch, err)
	}
}

type branchRow struct {
	ID     string
	Repo   string
	Branch string
	State  string
}

func listBranchesByRepo(ctx context.Context, db *state.DB, repo string) ([]branchRow, error) {
	rows, err := db.Unwrap().QueryContext(ctx, `
		SELECT id, repo, branch, state
		FROM branch_states
		WHERE repo = ?
		ORDER BY branch ASC`, repo)
	if err != nil {
		return nil, fmt.Errorf("list branches by repo: %w", err)
	}
	defer rows.Close()

	var out []branchRow
	for rows.Next() {
		var row branchRow
		if err := rows.Scan(&row.ID, &row.Repo, &row.Branch, &row.State); err != nil {
			return nil, fmt.Errorf("scan branch row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate branch rows: %w", err)
	}
	return out, nil
}

func seedAssignment(tb testing.TB, db *state.DB, sessionID, agentID, repo, branch, taskID string) *state.AgentAssignment {
	tb.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := state.RegisterAgentSession(ctx, db, sessionID, agentID, "cli", ""); err != nil {
		tb.Fatalf("register agent session: %v", err)
	}

	assignment := &state.AgentAssignment{
		ID:        fmt.Sprintf("%s-assignment", taskID),
		SessionID: sessionID,
		AgentID:   agentID,
		Repo:      repo,
		Branch:    branch,
		Worktree:  filepath.Join(tb.TempDir(), "worktree"),
		TaskID:    taskID,
	}
	if err := state.AttachAgentAssignment(ctx, db, assignment); err != nil {
		tb.Fatalf("attach assignment: %v", err)
	}
	return assignment
}
