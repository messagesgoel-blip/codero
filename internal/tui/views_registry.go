// Package tui provides the terminal-based operator interface for Codero.
//
// views_registry.go defines the canonical view identifiers required by
// Real-Time Views v1 §2.1. Each ViewID maps to a keybind-accessible view.
package tui

// ViewID identifies a TUI view in the view registry (§2.1).
type ViewID string

const (
	ViewOverview   ViewID = "overview"
	ViewSession    ViewID = "session"
	ViewGate       ViewID = "gate"
	ViewQueue      ViewID = "queue"
	ViewPipeline   ViewID = "pipeline"
	ViewEvents     ViewID = "events"
	ViewChat       ViewID = "chat"
	ViewBranch     ViewID = "branch"
	ViewArchives   ViewID = "archives"
	ViewCompliance ViewID = "compliance"
	ViewSettings   ViewID = "settings"
)

// AllViews returns every view defined in the spec §2.1 table.
func AllViews() []ViewID {
	return []ViewID{
		ViewOverview, ViewSession, ViewGate, ViewQueue,
		ViewPipeline, ViewEvents, ViewChat, ViewBranch,
		ViewArchives, ViewCompliance, ViewSettings,
	}
}

// ViewMeta holds display metadata for a view.
type ViewMeta struct {
	ID          ViewID
	Label       string
	DefaultBind string
	Implemented bool
}

// ViewRegistry returns metadata for every spec view, including whether
// the view is currently implemented in the TUI.
func ViewRegistry() []ViewMeta {
	return []ViewMeta{
		{ViewOverview, "Mission control", "o", true},
		{ViewSession, "Session drill-down", "s", false},
		{ViewGate, "Gate progress", "g", true},
		{ViewQueue, "Task queue", "q", true},
		{ViewPipeline, "Pipeline stages", "p", true},
		{ViewEvents, "Event stream", "e", true},
		{ViewChat, "LiteLLM chat", "c", true},
		{ViewBranch, "Branch details", "b", true},
		{ViewArchives, "Session archives", "a", false},
		{ViewCompliance, "Compliance rules", "r", false},
		{ViewSettings, "TUI settings", "/", false},
	}
}

// ImplementedViews returns only the views that have real TUI rendering.
func ImplementedViews() []ViewID {
	var out []ViewID
	for _, m := range ViewRegistry() {
		if m.Implemented {
			out = append(out, m.ID)
		}
	}
	return out
}
