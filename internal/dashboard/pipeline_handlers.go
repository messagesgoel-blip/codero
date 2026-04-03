package dashboard

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
)

// PipelineCard is one row in the dashboard pipeline view.
type PipelineCard struct {
	SessionID    string    `json:"session_id"`
	AssignmentID string    `json:"assignment_id,omitempty"`
	TaskID       string    `json:"task_id,omitempty"`
	AgentID      string    `json:"agent_id"`
	Repo         string    `json:"repo"`
	Branch       string    `json:"branch"`
	PRNumber     int       `json:"pr_number"`
	State        string    `json:"state,omitempty"`
	Substatus    string    `json:"substatus,omitempty"`
	Checkpoint   string    `json:"checkpoint"`
	Version      int       `json:"version"`
	StartedAt    time.Time `json:"started_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	StageSec     int64     `json:"stage_sec"`
}

// PipelineResponse is the response for GET /api/v1/dashboard/pipeline.
type PipelineResponse struct {
	Pipeline      []PipelineCard `json:"pipeline"`
	Cards         []PipelineCard `json:"cards,omitempty"`
	Total         int            `json:"total"`
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
}

func (h *Handler) handlePipeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	cards, err := queryPipeline(r.Context(), h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pipeline query failed", "db_error")
		return
	}
	if cards == nil {
		cards = []PipelineCard{}
	}
	writeJSON(w, http.StatusOK, PipelineResponse{
		Pipeline:      cards,
		Cards:         cards,
		Total:         len(cards),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func pipelineCardStageLabel(substatus, state string) string {
	if strings.TrimSpace(substatus) != "" {
		if stage := deriveCheckpointFromSubstatus(substatus); stage != "" {
			return stage
		}
	}
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked":
		return "MONITORING"
	case "completed":
		return "MERGED"
	case "active", "reviewing":
		return "MONITORING"
	default:
		return "SUBMITTED"
	}
}

func pipelineCardDuration(startedAt, updatedAt time.Time) int64 {
	if startedAt.IsZero() || updatedAt.IsZero() {
		return 0
	}
	if updatedAt.Before(startedAt) {
		return 0
	}
	return int64(updatedAt.Sub(startedAt).Seconds())
}

func nullStringValue(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}
