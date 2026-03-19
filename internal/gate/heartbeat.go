// Package gate provides the shared heartbeat gate client for pre-commit review.
// It implements the polling contract defined in the shared agent-toolkit
// gate-heartbeat script: first call starts the run (STATUS: PENDING), subsequent
// calls poll until STATUS: PASS or STATUS: FAIL.
package gate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultHeartbeatBin is the canonical path to the shared gate-heartbeat script.
const DefaultHeartbeatBin = "/srv/storage/shared/agent-toolkit/bin/gate-heartbeat"

// Default timeout values (seconds). All are independently configurable via env.
const (
	DefaultCopilotTimeoutSec   = 75 // only relevant when CODERO_COPILOT_ENABLED=true
	DefaultLiteLLMTimeoutSec   = 45
	DefaultGateTotalTimeoutSec = 180
	DefaultPollIntervalSec     = 180
)

// Status represents the terminal or intermediate gate state.
type Status string

const (
	StatusPending Status = "PENDING"
	StatusPass    Status = "PASS"
	StatusFail    Status = "FAIL"
)

// Result holds parsed output from a single gate-heartbeat invocation.
type Result struct {
	Status        Status
	RunID         string
	ElapsedSec    int
	PollAfterSec  int
	ProgressBar   string
	CurrentGate   string
	CopilotStatus string
	LiteLLMStatus string
	Comments      []string
}

// IsFinal reports whether the result represents a terminal state.
func (r Result) IsFinal() bool {
	return r.Status == StatusPass || r.Status == StatusFail
}

// Config holds gate timeout and binary configuration.
// All fields are populated from environment variables by LoadConfig.
type Config struct {
	// HeartbeatBin is the path to the gate-heartbeat script.
	HeartbeatBin string
	// CopilotEnabled controls whether the Copilot gate is run at all (CODERO_COPILOT_ENABLED).
	// Default false. When false, only LiteLLM is tried.
	CopilotEnabled bool
	// CopilotTimeoutSec is the per-gate Copilot timeout (CODERO_COPILOT_TIMEOUT_SEC).
	// Only relevant when CopilotEnabled is true.
	CopilotTimeoutSec int
	// LiteLLMTimeoutSec is the per-gate LiteLLM timeout (CODERO_LITELLM_TIMEOUT_SEC).
	// This timeout is fully independent of CopilotTimeoutSec.
	LiteLLMTimeoutSec int
	// GateTotalTimeoutSec is the overall gate wall-clock budget (CODERO_GATE_TOTAL_TIMEOUT_SEC).
	GateTotalTimeoutSec int
	// PollIntervalSec is the polling interval between heartbeat calls (CODERO_GATE_POLL_INTERVAL_SEC).
	PollIntervalSec int
	// RepoPath overrides the repo path passed to gate-heartbeat (CODERO_REPO_PATH).
	RepoPath string
}

// LoadConfig reads gate configuration from environment variables, falling back
// to built-in defaults. Each timeout is read independently so one gate's
// configuration cannot interfere with another gate's timeout budget.
func LoadConfig() Config {
	return Config{
		HeartbeatBin:        envOrDefault("CODERO_GATE_HEARTBEAT_BIN", DefaultHeartbeatBin),
		CopilotEnabled:      os.Getenv("CODERO_COPILOT_ENABLED") == "true",
		CopilotTimeoutSec:   envIntOrDefault("CODERO_COPILOT_TIMEOUT_SEC", DefaultCopilotTimeoutSec),
		LiteLLMTimeoutSec:   envIntOrDefault("CODERO_LITELLM_TIMEOUT_SEC", DefaultLiteLLMTimeoutSec),
		GateTotalTimeoutSec: envIntOrDefault("CODERO_GATE_TOTAL_TIMEOUT_SEC", DefaultGateTotalTimeoutSec),
		PollIntervalSec:     envIntOrDefault("CODERO_GATE_POLL_INTERVAL_SEC", DefaultPollIntervalSec),
		RepoPath:            os.Getenv("CODERO_REPO_PATH"),
	}
}

// ParseOutput parses key: value lines from gate-heartbeat stdout.
// Lines after "COMMENTS:" are treated as the blocker comment block.
func ParseOutput(output string) Result {
	r := Result{Status: StatusPending}
	scanner := bufio.NewScanner(strings.NewReader(output))
	inComments := false
	for scanner.Scan() {
		line := scanner.Text()
		if inComments {
			r.Comments = append(r.Comments, line)
			continue
		}

		// Handle "COMMENTS:" with or without a trailing value.
		if line == "COMMENTS:" {
			inComments = true
			continue
		}

		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			// Handle "KEY:" with no value (e.g. "COMMENTS:" already handled above).
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "STATUS":
			r.Status = Status(val)
		case "RUN_ID":
			r.RunID = val
		case "ELAPSED_SEC":
			r.ElapsedSec, _ = strconv.Atoi(val)
		case "POLL_AFTER_SEC":
			r.PollAfterSec, _ = strconv.Atoi(val)
		case "PROGRESS_BAR":
			r.ProgressBar = val
		case "CURRENT_GATE":
			r.CurrentGate = val
		case "COPILOT_STATUS":
			r.CopilotStatus = val
		case "LITELLM_STATUS":
			r.LiteLLMStatus = val
		case "COMMENTS":
			// "COMMENTS: none" means no blockers; anything else starts the block.
			if val != "none" && val != "" {
				r.Comments = append(r.Comments, val)
			}
			inComments = true
		}
	}
	return r
}

// Runner executes gate-heartbeat and polls until a final status (PASS or FAIL).
// It respects the timeout semantics defined in the heartbeat contract:
//   - CopilotTimeoutSec and LiteLLMTimeoutSec are per-gate and independent;
//     neither timeout reduces the other's budget.
//   - GateTotalTimeoutSec is the overall shell-managed budget.
//   - The Go context is used only for cancellation signals, not for timeout.
type Runner struct {
	Cfg Config
}

// Run invokes gate-heartbeat in a polling loop until the gate produces a final
// STATUS: PASS or STATUS: FAIL result. progressFn is called after each poll
// and may be nil.
func (r *Runner) Run(ctx context.Context, progressFn func(Result)) (Result, error) {
	env := buildEnv(r.Cfg)

	for {
		out, err := r.callHeartbeat(ctx, env)
		if err != nil {
			return Result{Status: StatusFail}, fmt.Errorf("gate-heartbeat: %w", err)
		}

		result := ParseOutput(out)
		if progressFn != nil {
			progressFn(result)
		}

		if result.IsFinal() {
			return result, nil
		}

		// Determine poll wait from the heartbeat response; fall back to config default.
		pollDur := time.Duration(result.PollAfterSec) * time.Second
		if pollDur <= 0 {
			pollDur = time.Duration(r.Cfg.PollIntervalSec) * time.Second
		}

		select {
		case <-ctx.Done():
			return Result{Status: StatusFail}, fmt.Errorf("gate cancelled: %w", ctx.Err())
		case <-time.After(pollDur):
		}
	}
}

func (r *Runner) callHeartbeat(ctx context.Context, env []string) (string, error) {
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, r.Cfg.HeartbeatBin) //nolint:gosec
	cmd.Env = env
	if r.Cfg.RepoPath != "" {
		cmd.Dir = r.Cfg.RepoPath
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// A non-zero exit can still be valid if heartbeat emitted STATUS output.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && strings.Contains(string(out), "STATUS:") {
			return string(out), nil
		}
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return "", fmt.Errorf("%w (output: %s)", err, trimmed)
		}
		return "", err
	}
	return string(out), nil
}

// buildEnv returns an env slice with gate-specific vars set from cfg,
// propagating the current process environment for all other vars.
func buildEnv(cfg Config) []string {
	env := os.Environ()
	copilotEnabled := "false"
	if cfg.CopilotEnabled {
		copilotEnabled = "true"
	}
	env = setEnvVar(env, "CODERO_COPILOT_ENABLED", copilotEnabled)
	env = setEnvVar(env, "CODERO_COPILOT_TIMEOUT_SEC", strconv.Itoa(cfg.CopilotTimeoutSec))
	env = setEnvVar(env, "CODERO_LITELLM_TIMEOUT_SEC", strconv.Itoa(cfg.LiteLLMTimeoutSec))
	env = setEnvVar(env, "CODERO_GATE_TOTAL_TIMEOUT_SEC", strconv.Itoa(cfg.GateTotalTimeoutSec))
	env = setEnvVar(env, "CODERO_GATE_POLL_INTERVAL_SEC", strconv.Itoa(cfg.PollIntervalSec))
	if cfg.RepoPath != "" {
		env = setEnvVar(env, "CODERO_REPO_PATH", cfg.RepoPath)
	}
	return env
}

// setEnvVar sets key=val in env, overwriting any existing entry for key.
func setEnvVar(env []string, key, val string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

// envOrDefault returns the environment variable value or def if unset/empty.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envIntOrDefault returns the environment variable parsed as a positive int,
// or def if unset, empty, or not a positive integer.
func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
