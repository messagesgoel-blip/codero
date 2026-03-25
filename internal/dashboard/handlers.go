package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codero/codero/internal/gate"
	loglib "github.com/codero/codero/internal/log"
)

const (
	maxUploadSize          = 10 << 20 // 10 MiB
	activityPageSize       = 50
	activeSessionsPageSize = 50
	runsPageSize           = 50
)

// allowedUploadExts contains the permitted file extensions for manual review upload.
var allowedUploadExts = map[string]bool{
	".py": true, ".ts": true, ".go": true, ".js": true,
	".diff": true, ".patch": true, ".rb": true, ".java": true,
}

// Handler is the HTTP handler collection for the dashboard API.
type Handler struct {
	db       *sql.DB
	settings *SettingsStore
}

// NewHandler creates a dashboard Handler backed by db and the given settings store.
func NewHandler(db *sql.DB, settings *SettingsStore) *Handler {
	return &Handler{db: db, settings: settings}
}

// RegisterRoutes mounts all dashboard routes onto mux.
// All routes sit under the prefix /api/v1/dashboard/.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/dashboard/overview", h.handleOverview)
	mux.HandleFunc("/api/v1/dashboard/repos", h.handleRepos)
	mux.HandleFunc("/api/v1/dashboard/runs", h.handleRuns)
	mux.HandleFunc("/api/v1/dashboard/activity", h.handleActivity)
	mux.HandleFunc("/api/v1/dashboard/block-reasons", h.handleBlockReasons)
	mux.HandleFunc("/api/v1/dashboard/gate-health", h.handleGateHealth)
	mux.HandleFunc("/api/v1/dashboard/active-sessions", h.handleActiveSessions)
	mux.HandleFunc("/api/v1/dashboard/assignments", h.handleAssignments)
	mux.HandleFunc("/api/v1/dashboard/agent-events", h.handleAgentEvents)
	mux.HandleFunc("/api/v1/dashboard/compliance", h.handleCompliance)
	mux.HandleFunc("/api/v1/dashboard/gate-checks", h.handleGateChecks)
	mux.HandleFunc("/api/v1/dashboard/health", h.handleDashboardHealth)
	mux.HandleFunc("/api/v1/dashboard/settings", h.handleSettings)
	mux.HandleFunc("/api/v1/dashboard/settings/gate-config", h.handleGateConfig)
	mux.HandleFunc("/api/v1/dashboard/settings/gate-config/", h.handleGateConfigVar)
	mux.HandleFunc("/api/v1/dashboard/chat", h.handleChat)
	mux.HandleFunc("/api/v1/dashboard/comments", h.handleChat)
	mux.HandleFunc("/api/v1/dashboard/manual-review-upload", h.handleUpload)
	mux.HandleFunc("/api/v1/dashboard/events", h.handleSSE)

	// §3 Session endpoints
	mux.HandleFunc("/api/v1/dashboard/sessions", h.handleSessions)
	mux.HandleFunc("/api/v1/dashboard/sessions/", h.handleSessionDetail)
	// §4 Assignment detail
	mux.HandleFunc("/api/v1/dashboard/assignments/", h.handleAssignmentDetail)
	// §5 Feedback
	mux.HandleFunc("/api/v1/dashboard/feedback/history", h.handleFeedbackHistory)
	mux.HandleFunc("/api/v1/dashboard/feedback/", h.handleFeedback)
	// §6 Gate
	mux.HandleFunc("/api/v1/dashboard/gate/", h.handleGateRouter)
	// §7 Merge
	mux.HandleFunc("/api/v1/dashboard/merge/", h.handleMergeRouter)
	// §8 Repo config
	mux.HandleFunc("/api/v1/dashboard/settings/repo-config/", h.handleRepoConfig)
	// §9 Compliance sub-endpoints
	mux.HandleFunc("/api/v1/dashboard/compliance/rules", h.handleComplianceRules)
	mux.HandleFunc("/api/v1/dashboard/compliance/checks/", h.handleComplianceChecks)
	mux.HandleFunc("/api/v1/dashboard/compliance/violations", h.handleComplianceViolations)
	// §10 Queue
	mux.HandleFunc("/api/v1/dashboard/queue", h.handleQueue)
	mux.HandleFunc("/api/v1/dashboard/queue/stats", h.handleQueueStats)
}

// handleOverview serves GET /api/v1/dashboard/overview.
func (h *Handler) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	runsToday, passedToday, blockedCount, avgGateSec, err := queryOverview(r.Context(), h.db)
	if err != nil {
		loglib.Error("dashboard: overview query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "overview query failed", "db_error")
		return
	}

	sparkline, err := querySparkline7d(r.Context(), h.db)
	if err != nil {
		loglib.Warn("dashboard: sparkline query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		sparkline = nil
	}

	var passRate float64 = -1
	if runsToday > 0 {
		passRate = float64(passedToday) / float64(runsToday) * 100
	}

	writeJSON(w, http.StatusOK, OverviewResponse{
		RunsToday:     runsToday,
		PassRate:      passRate,
		BlockedCount:  blockedCount,
		AvgGateSec:    avgGateSec,
		Sparkline7d:   sparkline,
		GeneratedAt:   time.Now().UTC(),
		SchemaVersion: SchemaVersionV1,
	})
}

// handleRepos serves GET /api/v1/dashboard/repos.
func (h *Handler) handleRepos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	repos, err := queryRepos(r.Context(), h.db)
	if err != nil {
		loglib.Error("dashboard: repos query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "repos query failed", "db_error")
		return
	}
	if repos == nil {
		repos = []RepoSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"repos":        repos,
		"generated_at": time.Now().UTC(),
	})
}

// handleRuns serves GET /api/v1/dashboard/runs.
func (h *Handler) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	runs, err := queryRuns(r.Context(), h.db, runsPageSize)
	if err != nil {
		loglib.Error("dashboard: runs query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "runs query failed", "db_error")
		return
	}
	if runs == nil {
		runs = []RunRow{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs":         runs,
		"generated_at": time.Now().UTC(),
	})
}

// handleActivity serves GET /api/v1/dashboard/activity.
func (h *Handler) handleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	events, err := queryActivity(r.Context(), h.db, activityPageSize)
	if err != nil {
		loglib.Error("dashboard: activity query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "activity query failed", "db_error")
		return
	}
	if events == nil {
		events = []ActivityEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events":       events,
		"generated_at": time.Now().UTC(),
	})
}

// handleBlockReasons serves GET /api/v1/dashboard/block-reasons.
func (h *Handler) handleBlockReasons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	reasons, err := queryBlockReasons(r.Context(), h.db)
	if err != nil {
		loglib.Error("dashboard: block-reasons query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "block-reasons query failed", "db_error")
		return
	}
	if reasons == nil {
		reasons = []BlockReason{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reasons":      reasons,
		"generated_at": time.Now().UTC(),
	})
}

// handleGateChecks serves GET /api/v1/dashboard/gate-checks.
// It reads the last gate-check report written by the `gate-check` CLI command.
// The report path is read from CODERO_GATE_CHECK_REPORT_PATH or the default
// .codero/gate-check/last-report.json relative to cwd.
func (h *Handler) handleGateChecks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	reportPath := gateCheckReportPath()

	data, err := os.ReadFile(reportPath) //nolint:gosec
	if err != nil {
		// No report yet — return an empty envelope so the dashboard can
		// distinguish "not yet run" from an actual error.
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"report":       nil,
			"message":      "no gate-check report available; run `codero gate-check` to generate one",
			"report_path":  reportPath,
			"generated_at": time.Now().UTC(),
		})
		return
	}

	// Parse the raw report into the dashboard model. We unmarshal into a generic
	// map and re-marshal to GateCheckReport so the dashboard model stays decoupled
	// from the gatecheck package (avoids a direct import dependency).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		loglib.Warn("dashboard: gate-check report parse failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to parse gate-check report", "parse_error")
		return
	}

	// Pass through the raw JSON with a generated_at wrapper so consumers get
	// the full canonical report plus a freshness timestamp.
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"report":       json.RawMessage(data),
		"report_path":  reportPath,
		"generated_at": time.Now().UTC(),
	})
}

// handleGateHealth serves GET /api/v1/dashboard/gate-health.
func (h *Handler) handleGateHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	health, err := queryGateHealth(r.Context(), h.db)
	if err != nil {
		loglib.Error("dashboard: gate-health query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "gate-health query failed", "db_error")
		return
	}
	if health == nil {
		health = []GateHealth{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"gates":        health,
		"generated_at": time.Now().UTC(),
	})
}

// handleActiveSessions serves GET /api/v1/dashboard/active-sessions.
func (h *Handler) handleActiveSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	sessions, err := queryActiveSessions(r.Context(), h.db, activeSessionsPageSize)
	if err != nil {
		loglib.Error("dashboard: active-sessions query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "active-sessions query failed", "db_error")
		return
	}
	if sessions == nil {
		sessions = []ActiveSession{}
	}
	writeJSON(w, http.StatusOK, ActiveSessionsResponse{
		ActiveCount:   len(sessions),
		Sessions:      sessions,
		GeneratedAt:   time.Now().UTC(),
		SchemaVersion: SchemaVersionV1,
	})
}

// handleAssignments serves GET /api/v1/dashboard/assignments.
func (h *Handler) handleAssignments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	assignments, err := queryAssignments(r.Context(), h.db, activeSessionsPageSize*4)
	if err != nil {
		loglib.Error("dashboard: assignments query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "assignments query failed", "db_error")
		return
	}
	if assignments == nil {
		assignments = []AssignmentSummary{}
	}
	writeJSON(w, http.StatusOK, AssignmentsResponse{
		Count:         len(assignments),
		Assignments:   assignments,
		GeneratedAt:   time.Now().UTC(),
		SchemaVersion: SchemaVersionV1,
	})
}

// handleAgentEvents serves GET /api/v1/dashboard/agent-events.
func (h *Handler) handleAgentEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	events, err := queryAgentEvents(r.Context(), h.db, activityPageSize*2)
	if err != nil {
		loglib.Error("dashboard: agent-events query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "agent-events query failed", "db_error")
		return
	}
	if events == nil {
		events = []AgentEventRow{}
	}
	writeJSON(w, http.StatusOK, AgentEventsResponse{
		Count:         len(events),
		Events:        events,
		GeneratedAt:   time.Now().UTC(),
		SchemaVersion: SchemaVersionV1,
	})
}

// handleCompliance serves GET /api/v1/dashboard/compliance.
func (h *Handler) handleCompliance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	rules, checks, err := queryCompliance(r.Context(), h.db, activeSessionsPageSize*8)
	if err != nil {
		loglib.Error("dashboard: compliance query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "compliance query failed", "db_error")
		return
	}
	if rules == nil {
		rules = []AgentRuleRow{}
	}
	if checks == nil {
		checks = []AssignmentRuleCheckRow{}
	}
	writeJSON(w, http.StatusOK, ComplianceResponse{
		Rules:         rules,
		Checks:        checks,
		GeneratedAt:   time.Now().UTC(),
		SchemaVersion: SchemaVersionV1,
	})
}

// handleDashboardHealth serves GET /api/v1/dashboard/health.
// It reports database connectivity, per-feed freshness, and active agent count
// without requiring access to the daemon's redis client or slot counter.
// For full service health (uptime, redis) see the /health endpoint.
func (h *Handler) handleDashboardHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)
	health, err := queryDashboardHealth(r.Context(), h.db)
	if err != nil {
		loglib.Error("dashboard: health query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "health query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, health)
}

// handleSettings serves GET and PUT /api/v1/dashboard/settings.
func (h *Handler) handleSettings(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	switch r.Method {
	case http.MethodGet:
		h.getSettings(w, r)
	case http.MethodPut:
		h.putSettings(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	ps, err := h.settings.Load()
	if err != nil {
		loglib.Error("dashboard: settings load failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "settings load failed", "settings_error")
		return
	}
	writeJSON(w, http.StatusOK, SettingsResponse{
		Integrations:  ps.Integrations,
		GatePipeline:  ps.GatePipeline,
		GeneratedAt:   time.Now().UTC(),
		SchemaVersion: SchemaVersionV1,
	})
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body", "read_error")
		return
	}

	var req SettingsUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "parse_error")
		return
	}

	if err := h.settings.Save(&req); err != nil {
		if isValidationError(err) {
			writeError(w, http.StatusUnprocessableEntity, err.Error(), "validation_error")
			return
		}
		loglib.Error("dashboard: settings save failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "settings save failed", "settings_error")
		return
	}

	loglib.Info("dashboard: settings updated",
		loglib.FieldEventType, "dashboard_settings_updated",
		loglib.FieldComponent, "dashboard",
	)

	ps, err := h.settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings reload failed", "settings_error")
		return
	}
	writeJSON(w, http.StatusOK, SettingsResponse{
		Integrations:  ps.Integrations,
		GatePipeline:  ps.GatePipeline,
		GeneratedAt:   time.Now().UTC(),
		SchemaVersion: SchemaVersionV1,
	})
}

// handleUpload serves POST /api/v1/dashboard/manual-review-upload.
func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("multipart parse failed (max %d MiB): %v", maxUploadSize>>20, err),
			"parse_error")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field in form", "missing_file")
		return
	}
	defer file.Close()

	if err := validateUploadFile(header); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), "invalid_file")
		return
	}

	// Drain the file to enforce size limit after multipart is parsed.
	if _, err := io.Copy(io.Discard, io.LimitReader(file, maxUploadSize+1)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read upload", "read_error")
		return
	}

	// Determine target repo from optional form field, fall back to "manual".
	repo := r.FormValue("repo")
	if repo == "" {
		repo = "manual"
	}
	branch := sanitizeBranchName(header.Filename)
	runID := uuid.New().String()

	if err := insertManualReviewRun(r.Context(), h.db, runID, repo, branch, ""); err != nil {
		loglib.Error("dashboard: insert manual review run failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create review run", "db_error")
		return
	}

	loglib.Info("dashboard: manual review upload accepted",
		loglib.FieldEventType, "dashboard_manual_upload",
		loglib.FieldComponent, "dashboard",
		"run_id", runID,
		"repo", repo,
		"branch", branch,
		"filename", header.Filename,
	)

	writeJSON(w, http.StatusAccepted, UploadResponse{
		RunID:   runID,
		Repo:    repo,
		Branch:  branch,
		Status:  "pending",
		Message: fmt.Sprintf("manual review queued for %s", header.Filename),
	})
}

// handleSSE serves GET /api/v1/dashboard/events as a Server-Sent Events stream.
// It tails delivery_events by polling every 2 seconds, pushing new events to
// connected clients. Clients that lose the connection cause the goroutine to exit.
func (h *Handler) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "SSE not supported", "sse_unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	setCORSHeaders(w)

	// Start from the current tip so the client only gets events from now on.
	sinceSeq, err := queryLatestActivitySeq(r.Context(), h.db)
	if err != nil {
		sinceSeq = 0
	}

	// Send initial "connected" comment so the browser registers the connection.
	// Safe: SSE control line, no HTML context; client parses event-stream protocol.
	// nosemgrep
	_, _ = io.WriteString(w, fmt.Sprintf(": connected seq=%d\n\n", sinceSeq))
	flusher.Flush()

	ctx := r.Context()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, err := queryActivitySince(ctx, h.db, sinceSeq, 20)
			if err != nil {
				// Transient DB error; keep the stream alive but log it.
				loglib.Warn("dashboard: SSE query error",
					loglib.FieldComponent, "dashboard", "error", err)
				continue
			}
			for _, ev := range events {
				data, _ := json.Marshal(ev)
				// Safe: JSON payload emitted in SSE frame, not rendered as HTML.
				// nosemgrep
				_, _ = io.WriteString(w, fmt.Sprintf("event: activity\ndata: %s\n\n", data))
				if ev.Seq > sinceSeq {
					sinceSeq = ev.Seq
				}
			}
			if len(events) > 0 {
				flusher.Flush()
			}
		}
	}
}

// validateUploadFile checks the file extension and size of an uploaded file.
func validateUploadFile(h *multipart.FileHeader) error {
	ext := strings.ToLower(filepath.Ext(h.Filename))
	if !allowedUploadExts[ext] {
		allowed := make([]string, 0, len(allowedUploadExts))
		for k := range allowedUploadExts {
			allowed = append(allowed, k)
		}
		return fmt.Errorf("unsupported file type %q; allowed: %s", ext, strings.Join(allowed, " "))
	}
	if h.Size > maxUploadSize {
		return fmt.Errorf("file too large (%d bytes); max %d MiB", h.Size, maxUploadSize>>20)
	}
	return nil
}

// sanitizeBranchName converts a filename into a safe branch-name-like string.
func sanitizeBranchName(filename string) string {
	base := filepath.Base(filename)
	// Replace spaces and path-unsafe chars with dashes.
	var sb strings.Builder
	for _, c := range base {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			sb.WriteRune(c)
		} else {
			sb.WriteRune('-')
		}
	}
	return "manual/" + sb.String()
}

// isValidationError returns true for errors originating from validateSettingsUpdate.
// We use a simple prefix heuristic since these are user-facing validation errors.
func isValidationError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "gate pipeline:") ||
		strings.HasPrefix(msg, "integrations:") ||
		msg == "request body required"
}

// ─── Gate Config V1 endpoints ─────────────────────────────────────────────

// GateConfigResponse is the response for GET /api/v1/dashboard/settings/gate-config.
// Matches Dashboard API v1 §8.1.
type GateConfigResponse struct {
	Checks        []GateConfigCheck  `json:"checks"`
	AISettings    map[string]string  `json:"ai_settings"`
	ConfigDrifts  []gate.ConfigDrift `json:"config_drifts"`
	AlwaysOn      []string           `json:"always_on"`
	GeneratedAt   time.Time          `json:"generated_at"`
	SchemaVersion string             `json:"schema_version"`
}

// GateConfigCheck is a single check entry in the gate-config response.
type GateConfigCheck struct {
	Name         string `json:"name"`
	Tier         string `json:"tier"`
	Enabled      bool   `json:"enabled"`
	EnvVar       string `json:"env_var"`
	DefaultValue string `json:"default_value"`
	CurrentValue string `json:"current_value"`
	Source       string `json:"source"`
}

// GateConfigUpdateRequest is the body for PUT /api/v1/dashboard/settings/gate-config/{var_name}.
type GateConfigUpdateRequest struct {
	Value string `json:"value"`
}

// handleGateConfig serves GET /api/v1/dashboard/settings/gate-config.
func (h *Handler) handleGateConfig(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	switch r.Method {
	case http.MethodGet:
		h.getGateConfig(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (h *Handler) getGateConfig(w http.ResponseWriter, r *http.Request) {
	cfgPath := gate.DefaultConfigFilePath()
	vars, err := gate.ResolveEffective(cfgPath)
	if err != nil {
		loglib.Error("dashboard: gate config load failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "gate config load failed", "config_error")
		return
	}

	drifts, _ := gate.DetectDrifts(cfgPath)
	if drifts == nil {
		drifts = []gate.ConfigDrift{}
	}

	checks := make([]GateConfigCheck, 0)
	aiSettings := make(map[string]string)

	for _, rv := range vars {
		switch rv.Tier {
		case gate.TierAISetting:
			aiSettings[rv.EnvVar] = rv.Value
		default:
			checks = append(checks, GateConfigCheck{
				Name:         envVarToCheckName(rv.EnvVar),
				Tier:         string(rv.Tier),
				Enabled:      rv.Value == "true",
				EnvVar:       rv.EnvVar,
				DefaultValue: rv.DefaultValue,
				CurrentValue: rv.Value,
				Source:       string(rv.Source),
			})
		}
	}

	writeJSON(w, http.StatusOK, GateConfigResponse{
		Checks:        checks,
		AISettings:    aiSettings,
		ConfigDrifts:  drifts,
		AlwaysOn:      gate.AlwaysOnChecks(),
		GeneratedAt:   time.Now().UTC(),
		SchemaVersion: SchemaVersionV1,
	})
}

// handleGateConfigVar serves PUT /api/v1/dashboard/settings/gate-config/{var_name}.
func (h *Handler) handleGateConfigVar(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	switch r.Method {
	case http.MethodPut:
		h.putGateConfigVar(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (h *Handler) putGateConfigVar(w http.ResponseWriter, r *http.Request) {
	// Extract var name from path: /api/v1/dashboard/settings/gate-config/{var_name}
	varName := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/settings/gate-config/")
	if varName == "" {
		writeError(w, http.StatusBadRequest, "variable name required in path", "missing_var")
		return
	}

	// DA-5: always-on checks cannot be disabled via the dashboard.
	for _, name := range gate.AlwaysOnChecks() {
		envName := "CODERO_" + strings.ToUpper(name) + "_ENABLED"
		if strings.ReplaceAll(envName, "-", "_") == varName {
			writeError(w, http.StatusForbidden, "always-on check cannot be disabled", "always_on")
			return
		}
	}

	entry := gate.LookupEntry(varName)
	if entry == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("unknown gate config variable: %s", varName), "unknown_var")
		return
	}

	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body", "read_error")
		return
	}

	var req GateConfigUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "parse_error")
		return
	}

	cfgPath := gate.DefaultConfigFilePath()
	if err := gate.SaveConfigVar(cfgPath, varName, req.Value); err != nil {
		if strings.Contains(err.Error(), "unknown variable") {
			writeError(w, http.StatusNotFound, err.Error(), "unknown_var")
			return
		}
		if strings.Contains(err.Error(), "must be") {
			writeError(w, http.StatusUnprocessableEntity, err.Error(), "validation_error")
			return
		}
		loglib.Error("dashboard: gate config save failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "gate config save failed", "config_error")
		return
	}

	loglib.Info("dashboard: gate config updated",
		loglib.FieldEventType, "gate_config_updated",
		loglib.FieldComponent, "dashboard",
		"var", varName,
		"value", req.Value,
	)

	// Return the updated check entry.
	vars, _ := gate.ResolveEffectiveMap(cfgPath)
	if rv, ok := vars[varName]; ok {
		writeJSON(w, http.StatusOK, GateConfigCheck{
			Name:         envVarToCheckName(rv.EnvVar),
			Tier:         string(rv.Tier),
			Enabled:      rv.Value == "true",
			EnvVar:       rv.EnvVar,
			DefaultValue: rv.DefaultValue,
			CurrentValue: rv.Value,
			Source:       string(rv.Source),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// envVarToCheckName converts e.g. "CODERO_GOVET_ENABLED" to "govet".
func envVarToCheckName(envVar string) string {
	name := strings.TrimPrefix(envVar, "CODERO_")
	name = strings.TrimSuffix(name, "_ENABLED")
	return strings.ToLower(name)
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		loglib.Warn("dashboard: response encode error",
			loglib.FieldComponent, "dashboard", "error", err)
	}
}

// writeError writes a standard JSON error envelope.
func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, ErrorResponse{Error: message, Code: code})
}

// setCORSHeaders adds permissive CORS headers for local development.
// In production the dashboard is served from the same origin so these are no-ops.
func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, OPTIONS")
}

// Ensure context is used to avoid unused import.
var _ = context.Background
