package dashboard_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestPipelineEndpoint_ReturnsCards(t *testing.T) {
	h, db := newTestHandler(t)
	now := time.Now().UTC()

	seedAgentSession(t, db, "sess-pipe", "agent-a", "cli", now.Add(-15*time.Minute), now.Add(-30*time.Second))
	seedAgentAssignmentWithSubstatus(t, db, "assign-pipe", "sess-pipe", "agent-a", "acme/api", "feat/pipeline", "wt-pipe", "task-pipe", "waiting_for_merge_approval", now.Add(-10*time.Minute))

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/pipeline", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Pipeline []struct {
			SessionID    string `json:"session_id"`
			AssignmentID string `json:"assignment_id"`
			TaskID       string `json:"task_id"`
			AgentID      string `json:"agent_id"`
			Checkpoint   string `json:"checkpoint"`
			Version      int    `json:"version"`
			StageSec     int64  `json:"stage_sec"`
		} `json:"pipeline"`
		Cards []struct {
			SessionID string `json:"session_id"`
		} `json:"cards"`
		Total         int    `json:"total"`
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SchemaVersion != "1" {
		t.Fatalf("schema_version=%q, want 1", resp.SchemaVersion)
	}
	if resp.Total != 1 || len(resp.Pipeline) != 1 || len(resp.Cards) != 1 {
		t.Fatalf("unexpected response sizes: total=%d pipeline=%d cards=%d", resp.Total, len(resp.Pipeline), len(resp.Cards))
	}
	card := resp.Pipeline[0]
	if card.SessionID != "sess-pipe" {
		t.Fatalf("session_id=%q, want sess-pipe", card.SessionID)
	}
	if card.AssignmentID != "assign-pipe" || card.TaskID != "task-pipe" {
		t.Fatalf("assignment data missing: %+v", card)
	}
	if card.Checkpoint != "PR_ACTIVE" {
		t.Fatalf("checkpoint=%q, want PR_ACTIVE", card.Checkpoint)
	}
	if card.AgentID != "agent-a" {
		t.Fatalf("agent_id=%q, want agent-a", card.AgentID)
	}
	if card.StageSec <= 0 {
		t.Fatalf("stage_sec=%d, want >0", card.StageSec)
	}
}
