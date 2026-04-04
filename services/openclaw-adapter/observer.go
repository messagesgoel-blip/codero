package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// observerConfig holds the configuration the observer needs.
type observerConfig struct {
	BaseURL          string
	BridgePath       string
	CoderoPath       string
	CoderoConfigPath string
	PollInterval     time.Duration
}

// observerSession is the subset of SessionRow fields the observer needs.
type observerSession struct {
	SessionID       string `json:"session_id"`
	TmuxSessionName string `json:"tmux_session_name"`
	Repo            string `json:"repo"`
	Branch          string `json:"branch"`
	Status          string `json:"status"`
}

// observerAuditEntry is logged for each TASK_COMPLETE observation.
type observerAuditEntry struct {
	Timestamp    time.Time `json:"ts"`
	Kind         string    `json:"kind"`
	SessionID    string    `json:"session_id"`
	TmuxSession  string    `json:"tmux_session"`
	Detected     bool      `json:"detected"`
	PRTitle      string    `json:"pr_title,omitempty"`
	UsedFallback bool      `json:"used_fallback"`
	SubmitStatus string    `json:"submit_status,omitempty"`
	SubmitError  string    `json:"submit_error,omitempty"`
}

// Observer polls active Codero sessions and triggers codero submit on TASK_COMPLETE.
type Observer struct {
	cfg        observerConfig
	httpClient *http.Client
	auditFile  *os.File
	auditMu    *sync.Mutex
	seen       map[string]string // session_id → last PTY content hash
}

func NewObserver(cfg observerConfig, auditFile *os.File, auditMu *sync.Mutex) *Observer {
	return &Observer{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		auditFile:  auditFile,
		auditMu:    auditMu,
		seen:       make(map[string]string),
	}
}

// Start launches the observer loop. It returns when ctx is cancelled.
func (o *Observer) Start(ctx context.Context) {
	interval := o.cfg.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("observer: started (poll every %s)", interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("observer: stopped")
			return
		case <-ticker.C:
			o.tick(ctx)
		}
	}
}

func (o *Observer) tick(ctx context.Context) {
	sessions, err := o.fetchActiveSessions(ctx)
	if err != nil {
		log.Printf("observer: fetch sessions: %v", err)
		return
	}
	for _, sess := range sessions {
		if sess.TmuxSessionName == "" {
			continue
		}
		o.checkSession(ctx, sess)
	}
}

func (o *Observer) fetchActiveSessions(ctx context.Context) ([]observerSession, error) {
	url := strings.TrimRight(o.cfg.BaseURL, "/") + "/api/v1/dashboard/sessions?status=active&limit=50"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sessions: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	var result struct {
		Sessions []observerSession `json:"sessions"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Sessions, nil
}

func (o *Observer) checkSession(ctx context.Context, sess observerSession) {
	output, err := o.capturePTY(ctx, sess.TmuxSessionName)
	if err != nil {
		log.Printf("observer: capture %s: %v", sess.TmuxSessionName, err)
		return
	}

	hash := hashContent(output)
	if prev, ok := o.seen[sess.SessionID]; ok && prev == hash {
		return // same output, skip
	}

	parsed := ParseTaskComplete(output)
	if !parsed.Detected {
		o.seen[sess.SessionID] = hash
		return
	}

	// New TASK_COMPLETE detected
	o.seen[sess.SessionID] = hash

	title := parsed.PRTitle
	if title == "" {
		title = "TASK_COMPLETE (auto-submit)"
	}

	body := ""
	if parsed.ChangeSummary != "" {
		body = parsed.ChangeSummary
	}
	if parsed.TestNotes != "" {
		if body != "" {
			body += "\n\n"
		}
		body += "Test notes: " + parsed.TestNotes
	}

	result := ExecSubmit(ctx, o.cfg.CoderoPath, o.cfg.CoderoConfigPath, submitArgs{
		Repo:   sess.Repo,
		Branch: sess.Branch,
		Title:  title,
		Body:   body,
	})

	status := "success"
	if result.ExitCode != 0 {
		status = "rejected"
	}

	o.writeAudit(observerAuditEntry{
		Timestamp:    time.Now().UTC(),
		Kind:         "task_complete_observer",
		SessionID:    sess.SessionID,
		TmuxSession:  sess.TmuxSessionName,
		Detected:     true,
		PRTitle:      parsed.PRTitle,
		UsedFallback: parsed.UsedFallback,
		SubmitStatus: status,
		SubmitError:  result.Error,
	})
	log.Printf("observer: session %s TASK_COMPLETE detected, submit %s", sess.SessionID, status)
}

func (o *Observer) capturePTY(ctx context.Context, tmuxSession string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, o.cfg.BridgePath,
		"capture", "--session", tmuxSession, "--lines", "200")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

func (o *Observer) writeAudit(entry observerAuditEntry) {
	if o.auditFile == nil {
		return
	}
	o.auditMu.Lock()
	defer o.auditMu.Unlock()
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("observer: audit marshal: %v", err)
		return
	}
	o.auditFile.Write(append(data, '\n'))
}

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
