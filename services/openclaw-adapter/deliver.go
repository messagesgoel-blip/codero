// services/openclaw-adapter/deliver.go
//
// OCL-021: POST /deliver — format findings and deliver to agent PTY
// via agent-tmux-bridge.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type deliverRequest struct {
	SessionID string    `json:"session_id"`
	Findings  []finding `json:"findings"`
	Source    string    `json:"source"`
}

type finding struct {
	Severity string `json:"severity"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	RuleID   string `json:"rule_id,omitempty"`
}

type deliverResponse struct {
	Status         string `json:"status"`
	SessionID      string `json:"session_id"`
	DeliveredCount int    `json:"delivered_count"`
	Error          string `json:"error,omitempty"`
}

// deliveryAuditEntry is logged to the JSONL audit file for each delivery attempt.
type deliveryAuditEntry struct {
	Timestamp    time.Time `json:"ts"`
	Kind         string    `json:"kind"`
	SessionID    string    `json:"session_id"`
	FindingCount int       `json:"finding_count"`
	Status       string    `json:"status"`
	Error        string    `json:"error,omitempty"`
}

// sessionLookup holds the fields we need from Codero's session detail endpoint.
type sessionLookup struct {
	SessionID       string `json:"session_id"`
	Mode            string `json:"mode"`
	TmuxSessionName string `json:"tmux_session_name"`
}

const maxDeliverFindings = 20

func (h *handler) handleDeliver(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		setCORS(w, r)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		setCORS(w, r)
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	setCORS(w, r)

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req deliverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.SessionID == "" {
		writeErr(w, http.StatusBadRequest, "session_id is required")
		return
	}

	sess, err := h.lookupSession(r.Context(), req.SessionID)
	if err != nil {
		h.auditDelivery(deliveryAuditEntry{
			Timestamp: time.Now().UTC(), Kind: "delivery",
			SessionID: req.SessionID, FindingCount: len(req.Findings),
			Status: "failed", Error: "session lookup: " + err.Error(),
		})
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}
	if sess.TmuxSessionName == "" {
		h.auditDelivery(deliveryAuditEntry{
			Timestamp: time.Now().UTC(), Kind: "delivery",
			SessionID: req.SessionID, FindingCount: len(req.Findings),
			Status: "failed", Error: "session has no tmux session",
		})
		writeErr(w, http.StatusNotFound, "session has no tmux session")
		return
	}

	msg := formatFindings(req.Findings, req.Source)

	if h.cfg.BridgePath == "" {
		log.Printf("deliver: BRIDGE_PATH not set, skipping delivery")
		h.auditDelivery(deliveryAuditEntry{
			Timestamp: time.Now().UTC(), Kind: "delivery",
			SessionID: req.SessionID, FindingCount: len(req.Findings),
			Status: "skipped", Error: "bridge path not configured",
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(deliverResponse{
			Status:         "skipped",
			SessionID:      req.SessionID,
			DeliveredCount: len(req.Findings),
		})
		return
	}

	if err := h.execBridge(r.Context(), sess.TmuxSessionName, sess.Mode, msg); err != nil {
		h.auditDelivery(deliveryAuditEntry{
			Timestamp: time.Now().UTC(), Kind: "delivery",
			SessionID: req.SessionID, FindingCount: len(req.Findings),
			Status: "failed", Error: err.Error(),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(deliverResponse{
			Status:    "failed",
			SessionID: req.SessionID,
			Error:     "bridge delivery failed",
		})
		return
	}

	h.auditDelivery(deliveryAuditEntry{
		Timestamp: time.Now().UTC(), Kind: "delivery",
		SessionID: req.SessionID, FindingCount: len(req.Findings),
		Status: "success",
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(deliverResponse{
		Status:         "success",
		SessionID:      req.SessionID,
		DeliveredCount: len(req.Findings),
	})
}

// lookupSession fetches session details from the Codero dashboard API.
func (h *handler) lookupSession(ctx context.Context, sessionID string) (*sessionLookup, error) {
	url := strings.TrimRight(h.cfg.BaseURL, "/") + "/api/v1/dashboard/sessions/" + sessionID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var sess sessionLookup
	if err := json.Unmarshal(body, &sess); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	return &sess, nil
}

// formatFindings builds a human-readable message from findings.
func formatFindings(findings []finding, source string) string {
	if len(findings) == 0 {
		return fmt.Sprintf("Codero findings for %s: no findings.", source)
	}

	sorted := make([]finding, len(findings))
	copy(sorted, findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		return findingSeverityRank(sorted[i].Severity) < findingSeverityRank(sorted[j].Severity)
	})

	var b strings.Builder
	total := len(sorted)
	display := sorted
	truncated := 0
	if total > maxDeliverFindings {
		display = sorted[:maxDeliverFindings]
		truncated = total - maxDeliverFindings
	}

	fmt.Fprintf(&b, "Codero findings for %s (%d total):\n", source, total)
	for _, f := range display {
		sev := strings.ToUpper(f.Severity)
		if sev == "" {
			sev = "INFO"
		}
		fmt.Fprintf(&b, "  [%s] %s:%d — %s\n", sev, f.File, f.Line, f.Message)
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "  ... and %d more\n", truncated)
	}
	return b.String()
}

func findingSeverityRank(s string) int {
	switch strings.ToLower(s) {
	case "error":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}

// execBridge calls the agent-tmux-bridge deliver command.
func (h *handler) execBridge(ctx context.Context, tmuxSession, profile, message string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, h.cfg.BridgePath,
		"deliver",
		"--session", tmuxSession,
		"--profile", profile,
		"--message-stdin",
		"--timeout", "15",
	)
	cmd.Stdin = strings.NewReader(message)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bridge: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// auditDelivery writes a delivery event to the JSONL audit log.
func (h *handler) auditDelivery(entry deliveryAuditEntry) {
	if h.auditFile == nil {
		return
	}
	h.auditMu.Lock()
	defer h.auditMu.Unlock()
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("audit: marshal delivery: %v", err)
		return
	}
	h.auditFile.Write(append(data, '\n'))
}
