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

// ActiveTask is the optional best-effort task context shown alongside a session.
type ActiveTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Phase string `json:"phase"`
}

// ActiveSession is one row in the active-sessions panel.
type ActiveSession struct {
	SessionID       string      `json:"session_id"`
	Repo            string      `json:"repo"`
	Branch          string      `json:"branch"`
	PRNumber        int         `json:"pr_number"`
	OwnerAgent      string      `json:"owner_agent"`
	ActivityState   string      `json:"activity_state"`
	Task            *ActiveTask `json:"task,omitempty"`
	StartedAt       time.Time   `json:"started_at"`
	LastHeartbeatAt time.Time   `json:"last_heartbeat_at"`
	ElapsedSec      int64       `json:"elapsed_sec"`
}

// ActiveSessionsResponse is the response for GET /api/v1/dashboard/active-sessions.
type ActiveSessionsResponse struct {
	ActiveCount int             `json:"active_count"`
	Sessions    []ActiveSession `json:"sessions"`
	GeneratedAt time.Time       `json:"generated_at"`
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

// ChatRequest is the body for POST /api/v1/dashboard/chat.
type ChatRequest struct {
	Prompt  string `json:"prompt"`
	Tab     string `json:"tab,omitempty"`
	Context string `json:"context,omitempty"`
	Stream  bool   `json:"stream,omitempty"`
}

// ChatSuggestion is a follow-up prompt returned by the assistant.
type ChatSuggestion struct {
	Label  string `json:"label"`
	Prompt string `json:"prompt"`
}

// ChatAction is an advisory next-step card returned with each assistant reply.
type ChatAction struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Prompt string `json:"prompt"`
	Tab    string `json:"tab,omitempty"`
}

// ChatResponse is the assistant response envelope for the dashboard chat API.
type ChatResponse struct {
	Reply       string           `json:"reply"`
	Provider    string           `json:"provider"`
	Model       string           `json:"model"`
	Suggestions []ChatSuggestion `json:"suggestions,omitempty"`
	Actions     []ChatAction     `json:"actions,omitempty"`
	GeneratedAt time.Time        `json:"generated_at"`
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

// ServiceStatus is the health state of a single backend service.
// Status is "ok", "degraded", or "down".
type ServiceStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// FeedStatus captures the freshness of a single data feed.
// Status is "ok", "stale", or "unavailable".
type FeedStatus struct {
	Status       string    `json:"status"`
	LastRefresh  time.Time `json:"last_refresh,omitempty"`
	FreshnessSec int64     `json:"freshness_sec"`
}

// DashboardFeeds holds per-feed freshness state.
type DashboardFeeds struct {
	ActiveSessions FeedStatus `json:"active_sessions"`
	GateChecks     FeedStatus `json:"gate_checks"`
}

// SecurityScoreStats holds the computed security score derived from the gate-check report.
type SecurityScoreStats struct {
	Score    int     `json:"score"`    // 0–10
	Pct      float64 `json:"pct"`      // 0–100
	Critical int     `json:"critical"` // required_failed count
	High     int     `json:"high"`     // failed count
	Total    int     `json:"total"`
}

// DashboardHealth is the response for GET /api/v1/dashboard/health.
// It reports DB connectivity, per-feed freshness, live agent count, and
// best-effort metrics derived from local files (security score, coverage, ETA).
type DashboardHealth struct {
	Database         ServiceStatus       `json:"database"`
	Feeds            DashboardFeeds      `json:"feeds"`
	ActiveAgentCount int                 `json:"active_agent_count"`
	SecurityScore    *SecurityScoreStats `json:"security_score,omitempty"`
	CoveragePct      *float64            `json:"coverage_pct,omitempty"`
	ETAMin           *int                `json:"eta_min,omitempty"`
	GeneratedAt      time.Time           `json:"generated_at"`
}

// GateCheckStatus mirrors gatecheck.CheckStatus for dashboard JSON.
type GateCheckStatus = string

// GateCheckResult is the dashboard representation of a single gate-check result.
// It mirrors gatecheck.CheckResult to avoid an import cycle in the dashboard layer.
type GateCheckResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Required    bool   `json:"required"`
	Enabled     bool   `json:"enabled"`
	Status      string `json:"status"`
	ReasonCode  string `json:"reason_code,omitempty"`
	Reason      string `json:"reason,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	ToolPath    string `json:"tool_path,omitempty"`
	ToolVersion string `json:"tool_version,omitempty"`
	DurationMS  int64  `json:"duration_ms"`
	Details     string `json:"details,omitempty"`
}

// GateCheckSummary mirrors gatecheck.Summary for the dashboard.
type GateCheckSummary struct {
	OverallStatus    string `json:"overall_status"`
	Passed           int    `json:"passed"`
	Failed           int    `json:"failed"`
	Skipped          int    `json:"skipped"`
	InfraBypassed    int    `json:"infra_bypassed"`
	Disabled         int    `json:"disabled"`
	Total            int    `json:"total"`
	RequiredFailed   int    `json:"required_failed"`
	RequiredDisabled int    `json:"required_disabled"`
	Profile          string `json:"profile"`
	SchemaVersion    string `json:"schema_version"`
}

// GateCheckReport is the top-level dashboard payload for GET /api/v1/dashboard/gate-checks.
type GateCheckReport struct {
	Summary     GateCheckSummary  `json:"summary"`
	Checks      []GateCheckResult `json:"checks"`
	RunAt       time.Time         `json:"run_at"`
	GeneratedAt time.Time         `json:"generated_at"`
}
