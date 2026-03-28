package gate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseProgressEnv converts KEY=VALUE pairs from progress.env content into a Result.
func ParseProgressEnv(content string) Result {
	fields := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	r := Result{
		RunID:         fields["RUN_ID"],
		ProgressBar:   fields["PROGRESS_BAR"],
		CurrentGate:   fields["CURRENT_GATE"],
		CopilotStatus: fields["COPILOT_STATUS"],
		LiteLLMStatus: fields["LITELLM_STATUS"],
	}

	switch fields["STATUS"] {
	case "PASS":
		r.Status = StatusPass
	case "FAIL":
		r.Status = StatusFail
	default:
		r.Status = StatusPending
	}

	if r.CopilotStatus == "" {
		r.CopilotStatus = "pending"
	}
	if r.LiteLLMStatus == "" {
		r.LiteLLMStatus = "pending"
	}

	if v, err := strconv.Atoi(fields["ELAPSED_SEC"]); err == nil {
		r.ElapsedSec = v
	}
	if v, err := strconv.Atoi(fields["POLL_AFTER_SEC"]); err == nil {
		r.PollAfterSec = v
	}
	if raw := fields["COMMENTS"]; raw != "" && raw != "none" {
		for _, c := range strings.Split(raw, "|") {
			if c = strings.TrimSpace(c); c != "" {
				r.Comments = append(r.Comments, c)
			}
		}
	}
	return r
}

// ReadProgressEnv reads .codero/gate-heartbeat/progress.env from repoRoot and
// returns a parsed Result. Returns a pending result if the file is missing.
// Returns a fail result with error details for other I/O errors.
func ReadProgressEnv(repoRoot string) Result {
	path := filepath.Join(repoRoot, ".codero", "gate-heartbeat", "progress.env")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{
				Status:        StatusPending,
				CopilotStatus: "pending",
				LiteLLMStatus: "pending",
			}
		}
		return Result{
			Status:        StatusFail,
			CopilotStatus: "error",
			LiteLLMStatus: "error",
			Comments:      []string{fmt.Sprintf("failed to read progress file: %v", err)},
		}
	}
	return ParseProgressEnv(string(data))
}
