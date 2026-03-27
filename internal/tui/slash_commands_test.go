package tui

import (
	"strings"
	"testing"
)

func TestSlashCommandRegistry(t *testing.T) {
	cmds := defaultSlashCommands()
	if len(cmds) == 0 {
		t.Fatal("expected at least one slash command")
	}
	names := make(map[string]bool)
	for _, cmd := range cmds {
		if cmd.Name == "" {
			t.Error("slash command with empty name")
		}
		if cmd.Description == "" {
			t.Errorf("slash command %q has empty description", cmd.Name)
		}
		if names[cmd.Name] {
			t.Errorf("duplicate slash command name: %s", cmd.Name)
		}
		names[cmd.Name] = true
	}
}

func TestFuzzyFilterSlashCommands(t *testing.T) {
	cmds := defaultSlashCommands()

	// Partial match
	filtered := fuzzyFilterCommands(cmds, "sta")
	found := false
	for _, c := range filtered {
		if c.Name == "status" {
			found = true
		}
	}
	if !found {
		t.Error("fuzzy filter for 'sta' should match 'status'")
	}

	// Empty filter returns all
	all := fuzzyFilterCommands(cmds, "")
	if len(all) != len(cmds) {
		t.Errorf("empty filter: got %d, want %d", len(all), len(cmds))
	}

	// No match returns empty
	none := fuzzyFilterCommands(cmds, "zzzzz")
	if len(none) != 0 {
		t.Errorf("non-matching filter: got %d, want 0", len(none))
	}

	// Case insensitive
	upper := fuzzyFilterCommands(cmds, "GATE")
	gateFound := false
	for _, c := range upper {
		if c.Name == "gate" {
			gateFound = true
		}
	}
	if !gateFound {
		t.Error("fuzzy filter should be case insensitive")
	}
}

func TestRenderSlashPopupContent(t *testing.T) {
	cmds := defaultSlashCommands()
	rendered := renderSlashPopupContent(DefaultTheme, cmds, 0, 40)
	if !strings.Contains(rendered, "/status") {
		t.Error("popup should contain /status")
	}
	if !strings.Contains(rendered, "session summary") {
		t.Error("popup should contain command descriptions")
	}
}
