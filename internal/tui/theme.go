package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/state"
)

// Theme centralises all style tokens.
type Theme struct {
	// Name identifies the theme variant (e.g. "dark", "light").
	Name   string
	Base   lipgloss.Style
	Bold   lipgloss.Style
	Muted  lipgloss.Style
	Accent lipgloss.Style

	Pass     lipgloss.Style
	Fail     lipgloss.Style
	Disabled lipgloss.Style
	Running  lipgloss.Style
	Pending  lipgloss.Style
	Warning  lipgloss.Style

	PaneBorder   lipgloss.Style
	PaneTitle    lipgloss.Style
	PaneHeader   lipgloss.Style
	ActiveBorder lipgloss.Style
	TabActive    lipgloss.Style
	TabInactive  lipgloss.Style

	ListSelected lipgloss.Style
	ListNormal   lipgloss.Style
	ListHeader   lipgloss.Style

	BottomBar lipgloss.Style
	KeyHint   lipgloss.Style
	KeyLabel  lipgloss.Style

	GateAuthoritative lipgloss.Style
	GatePipeline      lipgloss.Style

	Palette      lipgloss.Style
	PaletteInput lipgloss.Style

	ChipBackground lipgloss.Color
	ChipForeground lipgloss.Color

	Title lipgloss.Style
}

var DefaultTheme = newDefaultTheme()
var AltTheme = newAltTheme()

func newDefaultTheme() Theme {
	fg := lipgloss.Color("#CDD6F4")
	muted := lipgloss.Color("#6272A4")
	accent := lipgloss.Color("#BD93F9")
	pass := lipgloss.Color("#50FA7B")
	fail := lipgloss.Color("#FF5555")
	running := lipgloss.Color("#F1FA8C")
	warn := lipgloss.Color("#FFB86C")
	border := lipgloss.Color("#3C3C5A")
	activeBorder := lipgloss.Color("#7B68EE")
	selected := lipgloss.Color("#2A2B3D")
	paneTitle := lipgloss.Color("#BD93F9")
	bgPane := lipgloss.Color("#1E1F2E")
	bgPalette := lipgloss.Color("#1A1B26")
	headerBg := lipgloss.Color("#282A36")

	return Theme{
		Base:              lipgloss.NewStyle().Foreground(fg),
		Bold:              lipgloss.NewStyle().Foreground(fg).Bold(true),
		Muted:             lipgloss.NewStyle().Foreground(muted),
		Accent:            lipgloss.NewStyle().Foreground(accent).Bold(true),
		Pass:              lipgloss.NewStyle().Foreground(pass).Bold(true),
		Fail:              lipgloss.NewStyle().Foreground(fail).Bold(true),
		Disabled:          lipgloss.NewStyle().Foreground(muted),
		Running:           lipgloss.NewStyle().Foreground(running).Bold(true),
		Pending:           lipgloss.NewStyle().Foreground(muted),
		Warning:           lipgloss.NewStyle().Foreground(warn),
		PaneBorder:        lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(border),
		PaneTitle:         lipgloss.NewStyle().Foreground(paneTitle).Bold(true),
		PaneHeader:        lipgloss.NewStyle().Background(headerBg).Foreground(fg).Bold(true).Padding(0, 1),
		ActiveBorder:      lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(activeBorder),
		TabActive:         lipgloss.NewStyle().Foreground(fg).Bold(true).Underline(true),
		TabInactive:       lipgloss.NewStyle().Foreground(muted),
		ListSelected:      lipgloss.NewStyle().Background(selected).Foreground(fg).Bold(true),
		ListNormal:        lipgloss.NewStyle().Foreground(fg),
		ListHeader:        lipgloss.NewStyle().Foreground(paneTitle).Bold(true),
		BottomBar:         lipgloss.NewStyle().Background(bgPane).Foreground(muted),
		KeyHint:           lipgloss.NewStyle().Background(selected).Foreground(fg).Padding(0, 1),
		KeyLabel:          lipgloss.NewStyle().Foreground(muted),
		GateAuthoritative: lipgloss.NewStyle().Foreground(fg),
		GatePipeline:      lipgloss.NewStyle().Foreground(muted).Italic(true),
		Palette:           lipgloss.NewStyle().Background(bgPalette).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(activeBorder).Padding(0, 1),
		PaletteInput:      lipgloss.NewStyle().Foreground(fg),
		ChipBackground:    lipgloss.Color("#31384A"),
		ChipForeground:    lipgloss.Color("#A3A6B8"),
		Title:             lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
		Name:              "dark",
	}
}

func newAltTheme() Theme {
	fg := lipgloss.Color("#E8E8E8")
	muted := lipgloss.Color("#888888")
	accent := lipgloss.Color("#569CD6")
	pass := lipgloss.Color("#4EC9B0")
	fail := lipgloss.Color("#F44747")
	running := lipgloss.Color("#DCDCAA")
	warn := lipgloss.Color("#CE9178")
	border := lipgloss.Color("#444444")
	activeBorder := lipgloss.Color("#569CD6")
	selected := lipgloss.Color("#2D2D2D")
	paneTitle := lipgloss.Color("#9CDCFE")
	bgPane := lipgloss.Color("#1E1E1E")
	bgPalette := lipgloss.Color("#252526")
	headerBg := lipgloss.Color("#2D2D30")

	return Theme{
		Base:              lipgloss.NewStyle().Foreground(fg),
		Bold:              lipgloss.NewStyle().Foreground(fg).Bold(true),
		Muted:             lipgloss.NewStyle().Foreground(muted),
		Accent:            lipgloss.NewStyle().Foreground(accent).Bold(true),
		Pass:              lipgloss.NewStyle().Foreground(pass).Bold(true),
		Fail:              lipgloss.NewStyle().Foreground(fail).Bold(true),
		Disabled:          lipgloss.NewStyle().Foreground(muted),
		Running:           lipgloss.NewStyle().Foreground(running).Bold(true),
		Pending:           lipgloss.NewStyle().Foreground(muted),
		Warning:           lipgloss.NewStyle().Foreground(warn),
		PaneBorder:        lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(border),
		PaneTitle:         lipgloss.NewStyle().Foreground(paneTitle).Bold(true),
		PaneHeader:        lipgloss.NewStyle().Background(headerBg).Foreground(fg).Bold(true).Padding(0, 1),
		ActiveBorder:      lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(activeBorder),
		TabActive:         lipgloss.NewStyle().Foreground(fg).Bold(true).Underline(true),
		TabInactive:       lipgloss.NewStyle().Foreground(muted),
		ListSelected:      lipgloss.NewStyle().Background(selected).Foreground(fg).Bold(true),
		ListNormal:        lipgloss.NewStyle().Foreground(fg),
		ListHeader:        lipgloss.NewStyle().Foreground(paneTitle).Bold(true),
		BottomBar:         lipgloss.NewStyle().Background(bgPane).Foreground(muted),
		KeyHint:           lipgloss.NewStyle().Background(selected).Foreground(fg).Padding(0, 1),
		KeyLabel:          lipgloss.NewStyle().Foreground(muted),
		GateAuthoritative: lipgloss.NewStyle().Foreground(fg),
		GatePipeline:      lipgloss.NewStyle().Foreground(muted).Italic(true),
		Palette:           lipgloss.NewStyle().Background(bgPalette).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(activeBorder).Padding(0, 1),
		PaletteInput:      lipgloss.NewStyle().Foreground(fg),
		ChipBackground:    lipgloss.Color("#2D2D2D"),
		ChipForeground:    lipgloss.Color("#D4D4D4"),
		Title:             lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true),
		Name:              "light",
	}
}

// GateStatusStyle returns the theme style for a given gate state string.
func (t Theme) GateStatusStyle(gateState string) lipgloss.Style {
	switch gateState {
	case "pass":
		return t.Pass
	case "running":
		return t.Running
	case "blocked", "timeout":
		return t.Fail
	case "infra_fail":
		return t.Warning
	case "pending":
		return t.Pending
	default:
		return t.Muted
	}
}

// DisplayStateStyle returns the theme style for a LOG-001 display state
// ("passing", "failing", "disabled"). Falls back to Muted for unknown values.
func (t Theme) DisplayStateStyle(ds string) lipgloss.Style {
	switch ds {
	case "passing":
		return t.Pass
	case "failing":
		return t.Fail
	case "disabled":
		return t.Disabled
	default:
		return t.Muted
	}
}

// StateStyle returns the theme style for a branch lifecycle state.
func (t Theme) StateStyle(s state.State) lipgloss.Style {
	switch s {
	case state.StateMergeReady:
		return t.Pass
	case state.StateReviewApproved:
		return t.Accent
	case state.StateCLIReviewing, state.StateWaiting:
		return t.Running
	case state.StateQueuedCLI, state.StateExpired:
		return t.Warning
	case state.StateBlocked, state.StateStale, state.StateAbandoned:
		return t.Fail
	case state.StateSubmitted:
		return t.Base
	default:
		return t.Muted
	}
}
