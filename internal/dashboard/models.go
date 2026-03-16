package dashboard

import "time"

// OverviewResponse is the response for GET /api/v1/dashboard/overview.
type OverviewResponse struct {
	RunsToday    int        `json:"runs_today"`
	PassRate     float64    `json:"pass_rate"`     // 0–100, -1 if no data
	BlockedCount int        `json:"blocked_count"` // branches currently in blocked state
	AvgGateSec   float64    `json:"avg_gate_sec"`  // -1 if no data
	Sparkline7d  []DayStats `json:"sparkline_7d"`
	GeneratedAt  time.Time  `json:"generated_at"`
}

// DayStats is one day's worth of run stats for sparkline data.
type DayStats struct {
	Date   string `json:"date"` // YYYY-MM-DD
	Total  int    `json:"total"`
	Passed int    `json:"passed"`
	Failed int    `json:"failed"`
}

// RepoSummary is one row in the repos list.
type RepoSummary struct {
	Repo          string     `json:"repo"`
	Branch        string     `json:"branch"`
	State         string     `json:"state"`
	HeadHash      string     `json:"head_hash"`
	LastRunStatus string     `json:"last_run_status"` // "completed","failed","running","" if none
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
	GateSummary   []GatePill `json:"gate_summary"`
}

// GatePill is the per-provider status summary shown in the repos sidebar.
type GatePill struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pass","fail","run","idle"
}

// ActivityEvent is one item in the activity feed.
type ActivityEvent struct {
	Seq       int64     `json:"seq"`
	Repo      string    `json:"repo"`
	Branch    string    `json:"branch"`
	EventType string    `json:"event_type"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

// BlockReason is one ranked blocker in the block-reasons list.
type BlockReason struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
}

// GateHealth is per-provider pass-rate data.
type GateHealth struct {
	Provider string  `json:"provider"`
	Total    int     `json:"total"`
	Passed   int     `json:"passed"`
	PassRate float64 `json:"pass_rate"` // 0–100, -1 if no data
}

// RunRow is one row in the runs table.
type RunRow struct {
	ID         string     `json:"id"`
	Repo       string     `json:"repo"`
	Branch     string     `json:"branch"`
	HeadHash   string     `json:"head_hash"`
	Provider   string     `json:"provider"`
	Status     string     `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	Manual     bool       `json:"manual"`
	CreatedAt  time.Time  `json:"created_at"`
}

// SettingsResponse is the response for GET /api/v1/dashboard/settings.
type SettingsResponse struct {
	Integrations []IntegrationCard `json:"integrations"`
	GatePipeline []GateConfig      `json:"gate_pipeline"`
	GeneratedAt  time.Time         `json:"generated_at"`
}

// IntegrationCard is one integration shown on the settings page.
type IntegrationCard struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Desc      string `json:"desc"`
	Connected bool   `json:"connected"`
}

// GateConfig is one row in the gate pipeline table.
type GateConfig struct {
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	BlocksCommit bool   `json:"blocks_commit"`
	TimeoutSec   int    `json:"timeout_sec"`
	Provider     string `json:"provider"`
}

// SettingsUpdateRequest is the body for PUT /api/v1/dashboard/settings.
type SettingsUpdateRequest struct {
	Integrations []IntegrationCard `json:"integrations,omitempty"`
	GatePipeline []GateConfig      `json:"gate_pipeline,omitempty"`
}

// UploadResponse is the response for POST /api/v1/dashboard/manual-review-upload.
type UploadResponse struct {
	RunID   string `json:"run_id"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ErrorResponse is the common error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}
