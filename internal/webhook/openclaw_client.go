package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// OpenClawClient sends findings to the OpenClaw adapter for PTY delivery.
type OpenClawClient struct {
	adapterURL string
	httpClient *http.Client
}

// NewOpenClawClient creates a client pointing at the adapter base URL.
func NewOpenClawClient(adapterURL string, httpClient *http.Client) *OpenClawClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &OpenClawClient{adapterURL: adapterURL, httpClient: httpClient}
}

type adapterFinding struct {
	Severity string `json:"severity"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	RuleID   string `json:"rule_id,omitempty"`
}

type deliverPayload struct {
	SessionID string           `json:"session_id"`
	Findings  []adapterFinding `json:"findings"`
	Source    string           `json:"source"`
}

// Deliver sends findings to the adapter for PTY delivery.
// Fire-and-forget: logs warnings on failure, never returns error.
func (c *OpenClawClient) Deliver(ctx context.Context, sessionID string, findings []*state.FindingRecord, source string) error {
	if len(findings) == 0 {
		return nil
	}

	af := make([]adapterFinding, len(findings))
	for i, f := range findings {
		af[i] = adapterFinding{
			Severity: f.Severity,
			File:     f.File,
			Line:     f.Line,
			Message:  f.Message,
			RuleID:   f.RuleID,
		}
	}

	payload := deliverPayload{SessionID: sessionID, Findings: af, Source: source}
	body, err := json.Marshal(payload)
	if err != nil {
		loglib.Warn("openclaw: marshal deliver payload failed", "error", err)
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.adapterURL+"/deliver", bytes.NewReader(body))
	if err != nil {
		loglib.Warn("openclaw: build deliver request failed", "error", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		loglib.Warn("openclaw: deliver request failed",
			"error", err, "session_id", sessionID)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		loglib.Warn("openclaw: deliver returned error status",
			"status", resp.StatusCode, "session_id", sessionID)
	}
	return nil
}
