package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SlashCommand defines a chat slash command.
type SlashCommand struct {
	Name        string
	Description string
}

// defaultSlashCommands returns the built-in slash command registry.
func defaultSlashCommands() []SlashCommand {
	return []SlashCommand{
		{Name: "status", Description: "session summary"},
		{Name: "gate", Description: "gate-check details"},
		{Name: "queue", Description: "pipeline queue"},
		{Name: "clear", Description: "clear chat thread"},
		{Name: "agents", Description: "agent status"},
		{Name: "help", Description: "list all commands"},
	}
}

// fuzzyFilterCommands returns commands whose names contain the filter string.
func fuzzyFilterCommands(cmds []SlashCommand, filter string) []SlashCommand {
	if filter == "" {
		return cmds
	}
	filter = strings.ToLower(filter)
	var out []SlashCommand
	for _, c := range cmds {
		if strings.Contains(strings.ToLower(c.Name), filter) {
			out = append(out, c)
		}
	}
	return out
}

// renderSlashPopupContent renders the slash command list for the popup overlay.
func renderSlashPopupContent(theme Theme, cmds []SlashCommand, selectedIdx, width int) string {
	if len(cmds) == 0 {
		return theme.Muted.Render("  No matching commands")
	}

	maxNameLen := 0
	for _, c := range cmds {
		if len(c.Name) > maxNameLen {
			maxNameLen = len(c.Name)
		}
	}

	var lines []string
	for i, c := range cmds {
		prefix := "  "
		style := theme.Base
		if i == selectedIdx {
			prefix = "▸ "
			style = theme.ListSelected
		}
		name := fmt.Sprintf("/%-*s", maxNameLen, c.Name)
		line := prefix + style.Render(name) + "  " + theme.Muted.Render(c.Description)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")

	popupW := maxInt(1, minInt(width-4, 36))

	popup := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.PaneBorder.GetBorderBottomForeground()).
		Padding(0, 1).
		Width(popupW).
		Render(" " + theme.PaneTitle.Render("Commands") + "\n" + content)

	return popup
}
