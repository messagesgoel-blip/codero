package tui_test

import (
	"testing"

	"github.com/codero/codero/internal/tui"
)

func TestDefaultKeyMap(t *testing.T) {
	km := tui.DefaultKeyMap()

	bindings := []struct {
		name string
		keys []string
	}{
		{"Up", km.Up.Keys()},
		{"Down", km.Down.Keys()},
		{"Quit", km.Quit.Keys()},
		{"NextPane", km.NextPane.Keys()},
		{"NextTab", km.NextTab.Keys()},
		{"Palette", km.Palette.Keys()},
		{"Retry", km.Retry.Keys()},
		{"Logs", km.Logs.Keys()},
		{"Refresh", km.Refresh.Keys()},
		{"Chat", km.Chat.Keys()},
	}
	for _, b := range bindings {
		if len(b.keys) == 0 {
			t.Errorf("binding %q has no keys", b.name)
		}
	}
}

func TestShortHelp(t *testing.T) {
	km := tui.DefaultKeyMap()
	hints := km.ShortHelp()
	if len(hints) == 0 {
		t.Error("ShortHelp returned empty slice")
	}
}
