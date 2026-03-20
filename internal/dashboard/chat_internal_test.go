package dashboard

import (
	"strings"
	"testing"
	"time"
)

func TestDashboardChatPrompt_SanitizesSensitiveFields(t *testing.T) {
	now := time.Date(2026, time.March, 19, 12, 34, 56, 0, time.UTC)
	snapshot := dashboardChatSnapshot{
		Focus: "processes",
		Overview: dashboardFocus{
			RunsToday:    3,
			PassRate:     66.7,
			BlockedCount: 1,
			AvgGateSec:   12.5,
		},
		Health: DashboardHealth{
			Database: ServiceStatus{Status: "ok", Message: "do not leak this"},
			Feeds: DashboardFeeds{
				ActiveSessions: FeedStatus{Status: "ok", FreshnessSec: 7},
				GateChecks:     FeedStatus{Status: "stale", FreshnessSec: 11},
			},
			ActiveAgentCount: 2,
			GeneratedAt:      now,
		},
		ActiveSession: []ActiveSession{{
			SessionID:       "sess-1",
			Repo:            "acme/api",
			Branch:          "main",
			PRNumber:        42,
			OwnerAgent:      "Claude",
			ActivityState:   "reviewing",
			Task:            &ActiveTask{ID: "task-1", Title: "Fix auth", Phase: "review"},
			StartedAt:       now.Add(-15 * time.Minute),
			LastHeartbeatAt: now.Add(-30 * time.Second),
			ElapsedSec:      900,
		}},
		RecentEvents: []ActivityEvent{{
			Seq:       1,
			Repo:      "acme/api",
			Branch:    "main",
			EventType: "gate_check",
			Payload:   "token=abc123; secret=shh",
			CreatedAt: now,
		}},
		BlockReasons: []BlockReason{{Source: "lint", Count: 2}},
		GateChecks: &GateCheckReport{
			Summary: GateCheckSummary{
				OverallStatus: "fail",
				Passed:        3,
				Failed:        1,
				Total:         4,
				Profile:       "fast",
			},
			Checks: []GateCheckResult{{
				ID:          "check-1",
				Name:        "lint",
				Group:       "required",
				Required:    true,
				Enabled:     true,
				Status:      "fail",
				ReasonCode:  "whitespace",
				Reason:      "secret reason detail",
				ToolName:    "semgrep",
				ToolPath:    "redacted-tool-path",
				ToolVersion: "1.0.0",
				DurationMS:  120,
				Details:     "hidden details",
			}},
			RunAt:       now.Add(-2 * time.Minute),
			GeneratedAt: now,
		},
		GeneratedAt: now,
	}

	prompt := dashboardChatPrompt(ChatRequest{Prompt: "status", Tab: "processes"}, snapshot)
	for _, forbidden := range []string{"do not leak this", "token=abc123", "secret=shh", "secret reason detail", "hidden details", "redacted-tool-path", "tool_version", "1.0.0", "\"payload\"", "\"reason\"", "\"details\""} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt leaked %q: %s", forbidden, prompt)
		}
	}
	for _, required := range []string{"\"summary\"", "\"overall_status\": \"fail\"", "\"activity_state\": \"reviewing\""} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %s", required, prompt)
		}
	}
}

func TestDashboardChatStreamChunk_PreservesWhitespace(t *testing.T) {
	chunk := dashboardChatStreamChunk(liteLLMStreamResponse{Choices: []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Text string `json:"text"`
	}{
		{Delta: struct {
			Content string `json:"content"`
		}{Content: "  leading space"}},
	}})
	if chunk != "  leading space" {
		t.Fatalf("chunk = %q, want raw content", chunk)
	}
}
