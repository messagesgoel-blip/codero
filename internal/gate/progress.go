package gate

import "fmt"

// GateState represents the display state of a single review gate.
// Values correspond to the states written to progress.env by two-pass-review.sh.
type GateState string

const (
	GateStatePending   GateState = "pending"
	GateStateRunning   GateState = "running"
	GateStatePass      GateState = "pass"
	GateStateBlocked   GateState = "blocked"
	GateStateTimeout   GateState = "timeout"
	GateStateInfraFail GateState = "infra_fail"
)

// stateIcon maps a GateState to its single-character status icon.
// These icons are shared across CLI and TUI surfaces for visual parity.
var stateIcon = map[GateState]string{
	GateStatePending:   "○",
	GateStateRunning:   "●",
	GateStatePass:      "✓",
	GateStateBlocked:   "✗",
	GateStateTimeout:   "⏱",
	GateStateInfraFail: "!",
}

// StateIcon returns the display icon for a gate state string.
// Unknown states return "?".
func StateIcon(state string) string {
	if icon, ok := stateIcon[GateState(state)]; ok {
		return icon
	}
	return "?"
}

// RenderBar returns a single-line progress bar string that is identical
// across CLI and TUI surfaces:
//
//	[● copilot:running] [○ litellm:pending]
//
// If currentGate matches a gate name, that gate displays the running icon (●)
// regardless of its stored status value.
func RenderBar(copilotStatus, litellmStatus, currentGate string) string {
	return fmt.Sprintf("%s %s",
		renderGate("copilot", copilotStatus, currentGate == "copilot"),
		renderGate("litellm", litellmStatus, currentGate == "litellm"),
	)
}

func renderGate(name, status string, active bool) string {
	icon := StateIcon(status)
	if active {
		icon = stateIcon[GateStateRunning]
	}
	return fmt.Sprintf("[%s %s:%s]", icon, name, status)
}

// FormatProgressLine renders a compact single-line status for CLI polling display.
// Suitable for overwriting the current terminal line with \r.
func FormatProgressLine(r Result) string {
	bar := r.ProgressBar
	if bar == "" {
		bar = RenderBar(r.CopilotStatus, r.LiteLLMStatus, r.CurrentGate)
	}
	if r.ElapsedSec > 0 {
		return fmt.Sprintf("  %s  (%ds elapsed)  ", bar, r.ElapsedSec)
	}
	return fmt.Sprintf("  %s  ", bar)
}

// FormatSummary renders a multi-line result summary for display after gate completion.
// Used by both CLI (after commit-gate run) and TUI (progress pane).
func FormatSummary(r Result) string {
	bar := r.ProgressBar
	if bar == "" {
		bar = RenderBar(r.CopilotStatus, r.LiteLLMStatus, r.CurrentGate)
	}
	out := fmt.Sprintf("Gate:  %s\n", bar)
	if r.RunID != "" {
		out += fmt.Sprintf("RunID: %s\n", r.RunID)
	}
	if len(r.Comments) > 0 {
		out += "Blockers:\n"
		for _, c := range r.Comments {
			out += "  " + c + "\n"
		}
	}
	return out
}
