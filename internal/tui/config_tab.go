package tui

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ConfigVar is a single key-value pair in the config snapshot.
type ConfigVar struct {
	Key   string
	Value string
}

// ConfigSection groups related config variables under a title.
type ConfigSection struct {
	Title string
	Vars  []ConfigVar
}

// ConfigSnapshot holds all CODERO_* environment variables at startup.
type ConfigSnapshot struct {
	Sections []ConfigSection
}

// ConfigPane renders the config inspector tab.
type ConfigPane struct {
	theme    Theme
	snapshot ConfigSnapshot
	vp       viewport.Model
	width    int
	height   int
	ready    bool
}

// NewConfigPane creates a config pane with the given theme.
func NewConfigPane(theme Theme) ConfigPane {
	return ConfigPane{
		theme:    theme,
		snapshot: LoadConfigSnapshot(),
	}
}

func (p ConfigPane) Init() tea.Cmd { return nil }

func (p ConfigPane) Update(msg tea.Msg) (ConfigPane, tea.Cmd) {
	var cmd tea.Cmd
	if p.ready {
		p.vp, cmd = p.vp.Update(msg)
	}
	return p, cmd
}

func (p *ConfigPane) SetSize(w, h int) {
	p.width = w
	p.height = h
	if !p.ready {
		p.vp = viewport.New(w, h)
		p.ready = true
	} else {
		p.vp.Width = w
		p.vp.Height = h
	}
	p.vp.SetContent(p.renderContent())
}

func (p ConfigPane) View() string {
	if !p.ready {
		return "loading config..."
	}
	return p.vp.View()
}

// Refresh reloads the config snapshot from the environment.
func (p *ConfigPane) Refresh() {
	p.snapshot = LoadConfigSnapshot()
	if p.ready {
		p.vp.SetContent(p.renderContent())
	}
}

func (p ConfigPane) renderContent() string {
	t := p.theme
	var sb strings.Builder

	sb.WriteString(t.PaneTitle.Render("CONFIG INSPECTOR") + "\n")
	sb.WriteString(t.Muted.Render(strings.Repeat("─", p.width)) + "\n\n")

	for _, section := range p.snapshot.Sections {
		if len(section.Vars) == 0 {
			continue
		}
		// Section header
		sb.WriteString(t.PaneTitle.Render("  "+section.Title) + "\n")
		sb.WriteString(t.Muted.Render("  "+strings.Repeat("─", minInt(p.width-4, 60))) + "\n")

		// Variables
		for _, v := range section.Vars {
			label := t.Muted.Render(fmt.Sprintf("  %-28s", v.Key))
			value := v.Value
			if isSensitiveVar(v.Key) {
				value = "●●●●"
			}
			sb.WriteString(label + t.Base.Render(value) + "\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// LoadConfigSnapshot reads all CODERO_* environment variables and groups them.
func LoadConfigSnapshot() ConfigSnapshot {
	vars := make(map[string]string)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		if strings.HasPrefix(key, "CODERO_") {
			vars[key] = parts[1]
		}
	}
	return ConfigSnapshot{Sections: groupConfigVars(vars)}
}

// Explicit sensitive variable names.
var sensitiveVarExplicit = map[string]bool{
	"CODERO_CHAT_LITELLM_API_KEY": true,
	"CODERO_HEARTBEAT_SECRET":     true,
	"CODERO_LITELLM_API_KEY":      true,
	"CODERO_LITELLM_MASTER_KEY":   true,
	"CODERO_REDIS_PASS":           true,
	"CODERO_WEBHOOK_SECRET":       true,
}

var sensitiveVarSuffixRe = regexp.MustCompile(`_(KEY|SECRET|TOKEN|PASSWORD|PASS)$`)
var numericKeySuffixRe = regexp.MustCompile(`_KEY_\d+$`)

// isSensitiveVar returns true if the variable should be masked in the UI.
func isSensitiveVar(name string) bool {
	if sensitiveVarExplicit[name] {
		return true
	}
	if numericKeySuffixRe.MatchString(name) {
		return false
	}
	return sensitiveVarSuffixRe.MatchString(name)
}

// Section routing: map variable name prefixes to section titles.
var sessionContextVars = map[string]bool{
	"CODERO_REPO_PATH":    true,
	"CODERO_BRANCH":       true,
	"CODERO_TASK_ID":      true,
	"CODERO_SESSION_ID":   true,
	"CODERO_SESSION_MODE": true,
	"CODERO_AGENT_ID":     true,
	"CODERO_WORKTREE":     true,
}

func sectionForVar(key string) string {
	stripped := strings.TrimPrefix(key, "CODERO_")
	if sessionContextVars[key] {
		return "SESSION CONTEXT"
	}
	if strings.HasPrefix(stripped, "GATES_") || strings.HasPrefix(stripped, "REQUIRED_") ||
		strings.HasPrefix(stripped, "OPTIONAL_") || strings.HasPrefix(stripped, "MIN_AI_") ||
		strings.HasPrefix(stripped, "CHECK_") {
		return "GATE CONFIGURATION"
	}
	if strings.HasPrefix(stripped, "LITELLM_") || strings.HasPrefix(stripped, "DASHBOARD_") ||
		strings.HasPrefix(stripped, "REDIS_") || strings.HasPrefix(stripped, "GITHUB_") ||
		strings.HasPrefix(stripped, "WEBHOOK_") || strings.HasPrefix(stripped, "CHAT_LITELLM_") {
		return "INTEGRATIONS"
	}
	if strings.HasPrefix(stripped, "TUI_") {
		return "TUI SETTINGS"
	}
	if strings.HasPrefix(stripped, "CHAT_") {
		return "CHAT SETTINGS"
	}
	if strings.HasPrefix(stripped, "DAEMON_") || strings.HasPrefix(stripped, "HEARTBEAT_") {
		return "DAEMON"
	}
	return "OTHER"
}

// groupConfigVars organizes variables into titled sections.
func groupConfigVars(vars map[string]string) []ConfigSection {
	sectionOrder := []string{
		"SESSION CONTEXT", "GATE CONFIGURATION", "INTEGRATIONS",
		"TUI SETTINGS", "CHAT SETTINGS", "DAEMON", "OTHER",
	}
	groups := make(map[string][]ConfigVar)
	for key, value := range vars {
		section := sectionForVar(key)
		groups[section] = append(groups[section], ConfigVar{Key: key, Value: value})
	}
	var sections []ConfigSection
	for _, title := range sectionOrder {
		if cvars, ok := groups[title]; ok && len(cvars) > 0 {
			sort.Slice(cvars, func(i, j int) bool { return cvars[i].Key < cvars[j].Key })
			sections = append(sections, ConfigSection{Title: title, Vars: cvars})
		}
	}
	return sections
}

