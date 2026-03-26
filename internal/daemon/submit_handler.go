package daemon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	deliverypipeline "github.com/codero/codero/internal/delivery_pipeline"
)

// submitRequest is the JSON body for POST /api/v1/assignments/{id}/submit.
type submitRequest struct {
	SessionID string `json:"session_id"`
	Summary   string `json:"summary,omitempty"`
	Files     string `json:"files,omitempty"`
	Worktree  string `json:"worktree,omitempty"`
}

// handleSubmit handles POST /api/v1/assignments/{id}/submit.
//
// Response codes:
//
//	202 — submitted successfully
//	403 — session_id doesn't own the assignment
//	404 — assignment not found
//	409 — delivery pipeline already running (lock exists)
func (o *ObservabilityServer) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract assignment ID from URL path: /api/v1/assignments/{id}/submit
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/assignments/")
	assignmentID := strings.TrimSuffix(path, "/submit")
	if assignmentID == "" || assignmentID == path {
		http.Error(w, `{"error":"missing assignment id"}`, http.StatusBadRequest)
		return
	}

	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		http.Error(w, `{"error":"session_id required"}`, http.StatusBadRequest)
		return
	}

	if o.db == nil {
		http.Error(w, `{"error":"database not available"}`, http.StatusInternalServerError)
		return
	}

	// Look up the assignment.
	var ownerSessionID, worktree string
	err := o.db.QueryRowContext(r.Context(),
		`SELECT session_id, worktree FROM agent_assignments WHERE assignment_id = ? AND ended_at IS NULL`,
		assignmentID,
	).Scan(&ownerSessionID, &worktree)

	if err == sql.ErrNoRows {
		// Lazy assignment creation for first submit.
		var agentID string
		if err := o.db.QueryRowContext(r.Context(),
			`SELECT agent_id FROM agent_sessions WHERE session_id = ? AND ended_at IS NULL`,
			req.SessionID,
		).Scan(&agentID); err != nil {
			if err == sql.ErrNoRows {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(w, `{"error":"assignment not found"}`)
				return
			}
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}

		if strings.TrimSpace(req.Worktree) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":"assignment not found"}`)
			return
		}

		repo, branch, err := inferRepoBranch(req.Worktree)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"worktree inference failed: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		_, err = o.db.ExecContext(r.Context(),
			`INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch, worktree)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			assignmentID, req.SessionID, agentID, repo, branch, req.Worktree,
		)
		if err != nil {
			http.Error(w, `{"error":"create assignment failed"}`, http.StatusInternalServerError)
			return
		}
		ownerSessionID = req.SessionID
		worktree = req.Worktree
	} else if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	// Verify session ownership.
	if ownerSessionID != req.SessionID {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"error":"session does not own this assignment"}`)
		return
	}

	// Check delivery lock.
	if worktree != "" && deliverypipeline.IsLocked(worktree) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{"error":"delivery pipeline already running"}`)
		return
	}

	// Update delivery state atomically.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = o.db.ExecContext(r.Context(),
		`UPDATE agent_assignments
		 SET delivery_state = 'staging',
		     last_submit_at = ?,
		     revision_count = revision_count + 1
		 WHERE assignment_id = ?`,
		now, assignmentID,
	)
	if err != nil {
		http.Error(w, `{"error":"update failed"}`, http.StatusInternalServerError)
		return
	}

	// Acquire delivery lock if worktree is available.
	if worktree != "" {
		_ = deliverypipeline.Lock(worktree, req.SessionID, assignmentID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":        "submitted",
		"assignment_id": assignmentID,
	})
}

var inferRepoBranch = inferRepoBranchFromWorktree

func inferRepoBranchFromWorktree(worktree string) (string, string, error) {
	branch, err := runGit(worktree, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", err
	}
	remote, err := runGit(worktree, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", "", err
	}
	repo, err := normalizeRepoURL(strings.TrimSpace(remote))
	if err != nil {
		return "", "", err
	}
	return repo, strings.TrimSpace(branch), nil
}

func runGit(worktree string, args ...string) (string, error) {
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.Command("git", args...)
	cmd.Dir = worktree
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func normalizeRepoURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("remote origin url is empty")
	}
	switch {
	case strings.HasPrefix(raw, "git@"):
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("unexpected git ssh url: %s", raw)
		}
		return strings.TrimSuffix(parts[1], ".git"), nil
	case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"):
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse url: %w", err)
		}
		path := strings.TrimPrefix(u.Path, "/")
		path = strings.TrimSuffix(path, ".git")
		if path == "" || !strings.Contains(path, "/") {
			return "", fmt.Errorf("unexpected https url: %s", raw)
		}
		return path, nil
	default:
		return "", fmt.Errorf("unsupported remote url: %s", raw)
	}
}
