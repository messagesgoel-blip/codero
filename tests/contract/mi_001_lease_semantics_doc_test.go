package contract

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMI001LeaseContractDocExistsAndPinsStateMachine(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve runtime caller")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	path := filepath.Join(repoRoot, "docs", "contracts", "mi-001-lease-semantics.md")

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
