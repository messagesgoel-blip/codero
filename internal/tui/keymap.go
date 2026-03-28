package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all operator keyboard shortcuts.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding

	NextPane key.Binding
	PrevPane key.Binding

	NextTab key.Binding
	PrevTab key.Binding
	Tab1    key.Binding
	Tab2    key.Binding
	Tab3    key.Binding
	Tab4    key.Binding

	Retry    key.Binding
	Logs     key.Binding
	Chat     key.Binding
	Overview key.Binding
	Session  key.Binding
	Pipeline key.Binding
	Archives key.Binding
	Config   key.Binding

	Refresh key.Binding
	Quit    key.Binding
}

// DefaultKeyMap returns the default operator key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
		Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup/C-u", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn/C-d", "page down")),
		Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("home/g", "top")),
		End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("end/G", "bottom")),

		NextPane: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		PrevPane: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("S-tab", "prev pane")),

		NextTab: key.NewBinding(key.WithKeys("shift+right", "]"), key.WithHelp("]/S-→", "next tab")),
		PrevTab: key.NewBinding(key.WithKeys("shift+left", "["), key.WithHelp("[/S-←", "prev tab")),
		Tab1:    key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "logs")),
		Tab2:    key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "overview")),
		Tab3:    key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "events")),
		Tab4:    key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "queue")),

		Retry:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry gate")),
		Logs:     key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "logs")),
		Chat:     key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "chat")),
		Overview: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "overview")),
		Session:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "session drill")),
		Pipeline: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pipeline")),
		Archives: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archives")),
		Config:   key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "config")),

		Refresh: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("C-r", "refresh")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp returns the minimal key hints for the bottom bar.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Overview, k.Session, k.Pipeline, k.Archives, k.Config, k.NextPane, k.NextTab, k.Retry, k.Chat, k.Refresh, k.Quit}
}
