package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSessionsServer returns a test server that serves the sessions list API.
func fakeSessionsServer(t *testing.T, sessions []observerSession) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sessions": sessions,
			"total":    len(sessions),
		})
	}))
}

// fakeBridgeScript writes a shell script that outputs the given text when called.
func fakeBridgeScript(t *testing.T, dir, output string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-bridge")
	// Use printf to avoid trailing newline issues with echo
	script := "#!/bin/sh\nprintf '%s\\n' '" + strings.ReplaceAll(output, "'", "'\\''") + "'\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake bridge: %v", err)
	}
	return path
}

func TestObserver_DetectsTaskComplete(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	auditFile, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer auditFile.Close()

	ptyOutput := "some noise\nTASK_COMPLETE\npr_title: fix lint\nchange_summary: cleaned imports"
	bridgePath := fakeBridgeScript(t, dir, ptyOutput)
	coderoBin := writeFakeCodero(t, dir, 0, "Committed: abc1234")

	sessions := []observerSession{
		{SessionID: "sess-1", TmuxSessionName: "tmux-1", Repo: "owner/repo", Branch: "feat/x"},
	}
	srv := fakeSessionsServer(t, sessions)
	defer srv.Close()

	var mu sync.Mutex
	cfg := observerConfig{
		BaseURL:          srv.URL,
		BridgePath:       bridgePath,
		CoderoPath:       coderoBin,
		CoderoConfigPath: filepath.Join(dir, "test.yml"),
		PollInterval:     500 * time.Millisecond,
	}
	obs := NewObserver(cfg, auditFile, &mu)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	obs.Start(ctx)

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "task_complete_observer") {
		t.Errorf("audit log should contain task_complete_observer entry, got:\n%s", data)
	}
	if !strings.Contains(string(data), "fix lint") {
		t.Errorf("audit log should contain pr_title, got:\n%s", data)
	}
}

func TestObserver_NoDoublefire(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	auditFile, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer auditFile.Close()

	ptyOutput := "TASK_COMPLETE\npr_title: fix"
	bridgePath := fakeBridgeScript(t, dir, ptyOutput)
	coderoBin := writeFakeCodero(t, dir, 0, "ok")

	sessions := []observerSession{
		{SessionID: "sess-dup", TmuxSessionName: "tmux-dup", Repo: "o/r", Branch: "b"},
	}
	srv := fakeSessionsServer(t, sessions)
	defer srv.Close()

	var mu sync.Mutex
	cfg := observerConfig{
		BaseURL:          srv.URL,
		BridgePath:       bridgePath,
		CoderoPath:       coderoBin,
		CoderoConfigPath: filepath.Join(dir, "test.yml"),
		PollInterval:     500 * time.Millisecond,
	}
	obs := NewObserver(cfg, auditFile, &mu)

	// Run for 3 seconds — multiple poll ticks but same output
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	obs.Start(ctx)

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(string(data), "task_complete_observer")
	if count != 1 {
		t.Errorf("expected exactly 1 audit entry (no double-fire), got %d:\n%s", count, data)
	}
}

func TestObserver_NoSummaryBlock_Fallback(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	auditFile, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer auditFile.Close()

	ptyOutput := "TASK_COMPLETE"
	bridgePath := fakeBridgeScript(t, dir, ptyOutput)
	coderoBin := writeFakeCodero(t, dir, 0, "ok")

	sessions := []observerSession{
		{SessionID: "sess-fb", TmuxSessionName: "tmux-fb", Repo: "o/r", Branch: "b"},
	}
	srv := fakeSessionsServer(t, sessions)
	defer srv.Close()

	var mu sync.Mutex
	cfg := observerConfig{
		BaseURL:          srv.URL,
		BridgePath:       bridgePath,
		CoderoPath:       coderoBin,
		CoderoConfigPath: filepath.Join(dir, "test.yml"),
		PollInterval:     500 * time.Millisecond,
	}
	obs := NewObserver(cfg, auditFile, &mu)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	obs.Start(ctx)

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"used_fallback":true`) {
		t.Errorf("audit should show used_fallback=true, got:\n%s", data)
	}
}
