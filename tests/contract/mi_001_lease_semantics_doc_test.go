package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMI001LeaseContractDocExistsAndPinsStateMachine(t *testing.T) {
	path := filepath.Join(repoRoot(t), "docs", "contracts", "mi-001-lease-semantics.md")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected lease semantics contract doc: %v", err)
	}

	content := string(data)
	required := []string{
		"v4 Canonical State Machine table",
		"10 states",
		"20 transitions",
		"SET NX EX",
	}

	for _, token := range required {
		if !strings.Contains(content, token) {
			t.Fatalf("contract doc missing required token %q", token)
		}
	}
}
