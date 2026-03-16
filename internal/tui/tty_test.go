package tui_test

import (
	"testing"

	"github.com/codero/codero/internal/tui"
)

// TestIsInteractiveTTY_NonInteractive verifies that IsInteractiveTTY returns
// false when running under `go test` (stdin/stdout are pipes, not a terminal).
func TestIsInteractiveTTY_NonInteractive(t *testing.T) {
	// In test execution stdin/stdout are not a real TTY.
	if tui.IsInteractiveTTY() {
		t.Error("IsInteractiveTTY() returned true in a non-interactive test context")
	}
}
