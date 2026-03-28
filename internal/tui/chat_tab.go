package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/dashboard"
)

// renderChatTab renders the Codex-style chat tab for the center pane.
// Layout: session header, scrollable transcript, fixed 3-line composer.
func (m Model) renderChatTab(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	t := m.theme

	header := m.renderSessionHeader(width)
	headerH := strings.Count(header, "\n") + 1

	composer := m.renderChatComposer(width)
	composerH := strings.Count(composer, "\n") + 1

	transcriptH := height - headerH - composerH
	if transcriptH < 1 {
		transcriptH = 1
	}

	transcript := m.renderChatTranscript(width, transcriptH)

	// If slash popup is active, overlay it on top of the transcript.
	if m.chatState.SlashPopupActive {
		cmds := fuzzyFilterCommands(defaultSlashCommands(), m.chatState.SlashPopupFilter)
		popup := renderSlashPopupContent(t, cmds, m.chatState.SlashPopupIdx, width)
		popupLines := strings.Split(popup, "\n")
		transcriptLines := strings.Split(transcript, "\n")

		// Place the popup at the bottom of the transcript, just above composer.
		startLine := len(transcriptLines) - len(popupLines)
		if startLine < 0 {
			startLine = 0
		}
		for i, pl := range popupLines {
			idx := startLine + i
			if idx < len(transcriptLines) {
				transcriptLines[idx] = pl
			}
		}
		transcript = strings.Join(transcriptLines, "\n")
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, transcript, composer)
}

// renderSessionHeader renders a bordered box showing model name and scope.
func (m Model) renderSessionHeader(width int) string {
	t := m.theme
	model := os.Getenv("CODERO_CHAT_LITELLM_MODEL")
	if model == "" {
		model = "gpt-4o"
	}
	scope := m.chatContextTab()
	if scope == "" {
		scope = "all"
	}

	line1 := t.Accent.Render(">_") + " " + t.Bold.Render("Codero Chat") +
		" " + t.Muted.Render("(litellm/"+model+")")
	line2 := t.Muted.Render("model: "+model) + "    " + t.Muted.Render("scope: "+scope)

	innerW := width - 4 // borders + padding
	if innerW < 10 {
		innerW = 10
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#21262D")).
		Width(innerW).
		Padding(0, 1).
		Render(line1 + "\n" + line2)

	return box
}

// renderChatTranscript renders the scrollable message transcript.
func (m Model) renderChatTranscript(width, height int) string {
	if height < 1 {
		return ""
	}
	t := m.theme

	if len(m.cliMessages) == 0 {
		empty := t.Muted.Render("  Type a message or use /commands to interact with Codero.")
		lines := []string{"", empty}
		for len(lines) < height {
			lines = append(lines, "")
		}
		return strings.Join(lines[:height], "\n")
	}

	// Render messages bottom-up to fill available height.
	var allLines []string
	for _, msg := range m.cliMessages {
		rendered := m.renderCodexMessage(msg, width)
		msgLines := strings.Split(rendered, "\n")
		allLines = append(allLines, msgLines...)
	}

	// Take the last `height` lines.
	if len(allLines) > height {
		allLines = allLines[len(allLines)-height:]
	}
	for len(allLines) < height {
		allLines = append([]string{""}, allLines...)
	}

	return strings.Join(allLines, "\n")
}

// renderCodexMessage renders a single message with Codex-style prefix.
func (m Model) renderCodexMessage(msg terminalMessage, width int) string {
	t := m.theme
	var prefix string
	var prefixStyle lipgloss.Style

	switch msg.Role {
	case "user":
		prefix = "\u203a " // ›
		prefixStyle = t.Bold.Faint(true)
	case "assistant":
		prefix = "\u2022 " // •
		prefixStyle = lipgloss.NewStyle().Faint(true)
	case "error":
		prefix = "\u2717 " // ✗
		prefixStyle = t.Fail
	case "system":
		prefix = "  "
		prefixStyle = t.Bold
	default:
		prefix = "  "
		prefixStyle = t.Base
	}

	contentWidth := width - 4
	if contentWidth < 10 {
		contentWidth = 10
	}
	wrapped := simpleWordWrap(msg.Content, contentWidth)
	lines := strings.Split(wrapped, "\n")

	var result []string
	for i, line := range lines {
		if i == 0 {
			result = append(result, "  "+prefixStyle.Render(prefix)+t.Base.Render(line))
		} else {
			result = append(result, "    "+t.Base.Render(line))
		}
	}
	return strings.Join(result, "\n")
}

// renderChatComposer renders the 3-line composer at the bottom of the chat tab.
func (m Model) renderChatComposer(width int) string {
	t := m.theme

	// Line 1: suggestion chips
	suggestions := m.cliSuggestions
	if len(suggestions) == 0 {
		suggestions = []dashboard.ChatSuggestion{
			{Label: "status", Prompt: "status"},
			{Label: "gate", Prompt: "gate"},
			{Label: "queue", Prompt: "queue"},
			{Label: "clear", Prompt: "clear"},
		}
	}
	chips := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = strings.TrimSpace(s.Prompt)
		}
		if label != "" {
			chips = append(chips, commandChip(t, label))
		}
	}
	chipLine := "  " + strings.Join(chips, " ")

	// Line 2: prompt + input + busy indicator
	// Clamp input view to center-pane width to prevent overflow.
	inputView := m.cliInput.View()
	inputMaxW := maxInt(1, width-6) // account for "  ❯ " prefix + margin
	if lipgloss.Width(inputView) > inputMaxW {
		runes := []rune(inputView)
		for len(runes) > 0 && lipgloss.Width(string(runes)) > inputMaxW {
			runes = runes[:len(runes)-1]
		}
		inputView = string(runes)
	}
	input := t.Accent.Render("  \u276f") + " " + t.PaletteInput.Render(inputView)
	if m.cliBusy {
		input += " " + t.Warning.Render("\u25cf thinking\u2026")
	}

	// Line 3: key hints left, token counter right
	used, total := m.estimateTokenUsageApprox()
	pct := 0
	if total > 0 {
		pct = 100 - (used*100)/total
		if pct < 0 {
			pct = 0
		}
	}
	leftHints := t.Muted.Render("  /cmd  Esc back  \u2191\u2193 history")
	rightCounter := t.Muted.Render(fmt.Sprintf("%d%% context left  ", pct))
	spacerW := width - lipgloss.Width(leftHints) - lipgloss.Width(rightCounter)
	if spacerW < 1 {
		spacerW = 1
	}
	footerLine := leftHints + strings.Repeat(" ", spacerW) + rightCounter

	return lipgloss.JoinVertical(lipgloss.Left, chipLine, input, footerLine)
}

// estimateTokenUsageApprox estimates token usage as len(content)/4.
func (m Model) estimateTokenUsageApprox() (used, total int) {
	envTotal := os.Getenv("CODERO_CHAT_MAX_CONTEXT_SIZE")
	total = 128000
	if envTotal != "" {
		if v, err := strconv.Atoi(envTotal); err == nil && v > 0 {
			total = v
		}
	}

	for _, msg := range m.cliMessages {
		used += len(msg.Content) / 4
	}
	return used, total
}

// handleChatTabKey handles key input when TabChat is active.
func (m Model) handleChatTabKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Tab-switching keys work from the chat tab only when the input is empty,
	// so printable characters (], [, 1-4) reach the composer when typing.
	if strings.TrimSpace(m.cliInput.Value()) == "" {
		switch {
		case key.Matches(msg, m.keys.NextTab):
			m.activeTab = Tab((int(m.activeTab) + 1) % int(tabCount))
			m.focused = PaneCenter
			return m, nil
		case key.Matches(msg, m.keys.PrevTab):
			m.activeTab = Tab((int(m.activeTab) - 1 + int(tabCount)) % int(tabCount))
			m.focused = PaneCenter
			return m, nil
		case key.Matches(msg, m.keys.Tab1):
			m.activeTab = TabLogs
			m.focused = PaneCenter
			return m, nil
		case key.Matches(msg, m.keys.Tab2):
			m.activeTab = TabOverview
			m.focused = PaneCenter
			return m, nil
		case key.Matches(msg, m.keys.Tab3):
			m.activeTab = TabEvents
			m.focused = PaneCenter
			return m, nil
		case key.Matches(msg, m.keys.Tab4):
			m.activeTab = TabQueue
			m.focused = PaneCenter
			return m, nil
		}
	}

	switch msg.String() {
	case "esc":
		m.activeTab = m.prevTab
		m.focused = m.prevFocus
		return m, nil
	case "/":
		// Only open popup if the input is empty.
		if strings.TrimSpace(m.cliInput.Value()) == "" {
			m.chatState.SlashPopupActive = true
			m.chatState.SlashPopupFilter = ""
			m.chatState.SlashPopupIdx = 0
			return m, nil
		}
	}
	return m.handleTerminalKey(msg)
}

// handleSlashPopupKey handles key input when the slash popup is open.
func (m Model) handleSlashPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmds := fuzzyFilterCommands(defaultSlashCommands(), m.chatState.SlashPopupFilter)

	switch msg.String() {
	case "esc":
		m.chatState.SlashPopupActive = false
		m.chatState.SlashPopupFilter = ""
		m.chatState.SlashPopupIdx = 0
		return m, nil

	case "enter":
		m.chatState.SlashPopupActive = false
		if len(cmds) > 0 && m.chatState.SlashPopupIdx < len(cmds) {
			selected := cmds[m.chatState.SlashPopupIdx]
			m.chatState.SlashPopupFilter = ""
			m.chatState.SlashPopupIdx = 0

			// Execute the selected command as if the user typed it.
			m.cliMessages = append(m.cliMessages, terminalMessage{Role: "user", Content: selected.Name})
			m.truncateCLIHistory()
			if handled, msgs, suggestions, actions := m.localTerminalCommand(selected.Name); handled {
				if strings.EqualFold(selected.Name, "clear") {
					m.cliMessages = nil
					m.cliSuggestions = nil
					m.cliActions = nil
					m.chatConversationID = ""
					m.cliBusy = false
					return m, nil
				}
				if len(msgs) > 0 {
					m.cliMessages = append(m.cliMessages, msgs...)
					m.cliSuggestions = suggestions
					m.cliActions = actions
					m.truncateCLIHistory()
				}
				m.cliBusy = false
				return m, nil
			}
			// Command not handled locally — route to backend.
			m.cliBusy = true
			m.cliSuggestions = nil
			m.cliActions = nil
			m.cliMessages = append(m.cliMessages, terminalMessage{Role: "assistant", Meta: "streaming", Content: "…"})
			return m, dashboardChatStreamCmd(m.cfg.Context, selected.Name, m.chatContextTab(), m.chatConversationID)
		}
		return m, nil

	case "up", "k":
		if m.chatState.SlashPopupIdx > 0 {
			m.chatState.SlashPopupIdx--
		}
		return m, nil

	case "down", "j":
		if m.chatState.SlashPopupIdx < len(cmds)-1 {
			m.chatState.SlashPopupIdx++
		}
		return m, nil

	case "backspace":
		if len(m.chatState.SlashPopupFilter) > 0 {
			m.chatState.SlashPopupFilter = m.chatState.SlashPopupFilter[:len(m.chatState.SlashPopupFilter)-1]
			m.chatState.SlashPopupIdx = 0
		}
		return m, nil

	default:
		// Printable characters append to the filter.
		s := msg.String()
		if len(s) == 1 && s[0] >= 0x20 && s[0] <= 0x7E {
			m.chatState.SlashPopupFilter += s
			m.chatState.SlashPopupIdx = 0
		}
		return m, nil
	}
}

// simpleWordWrap wraps text to the given width preserving indentation.
// Operates on runes to handle multi-byte UTF-8 characters correctly.
func simpleWordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var result strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			result.WriteByte('\n')
		}
		runes := []rune(line)
		if len(runes) <= width {
			result.WriteString(line)
			continue
		}
		// Find leading whitespace to preserve indentation.
		indent := 0
		for indent < len(runes) && (runes[indent] == ' ' || runes[indent] == '\t') {
			indent++
		}
		pos := 0
		for pos < len(runes) {
			segWidth := width
			if pos > 0 && indent > 0 {
				segWidth = width - indent
				if segWidth < 10 {
					segWidth = 10
				}
			}
			end := pos + segWidth
			if end >= len(runes) {
				if pos > 0 {
					result.WriteByte('\n')
					if indent > 0 {
						result.WriteString(string(runes[:indent]))
					}
				}
				result.WriteString(string(runes[pos:]))
				break
			}
			// Find last space within the segment for a soft break.
			breakAt := -1
			for j := end; j > pos; j-- {
				if runes[j] == ' ' {
					breakAt = j
					break
				}
			}
			if breakAt <= pos {
				breakAt = end
			}
			if pos > 0 {
				result.WriteByte('\n')
				if indent > 0 {
					result.WriteString(string(runes[:indent]))
				}
			}
			result.WriteString(string(runes[pos:breakAt]))
			pos = breakAt
			if pos < len(runes) && runes[pos] == ' ' {
				pos++
			}
		}
	}
	return result.String()
}
