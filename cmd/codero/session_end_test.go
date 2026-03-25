package main

import (
	"testing"
)

// TestCert_SLv1_SessionEndCmdExists verifies SL-7: the codero session end
// subcommand exists and is properly registered.
//
// Matrix clause: SL-7 | Evidence: CT
func TestCert_SLv1_SessionEndCmdExists(t *testing.T) {
	var configPath string
	cmd := sessionCmd(&configPath)

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "end" {
			found = true
			if sub.Short == "" {
				t.Error("session end command missing short description")
			}
			// Verify required flags exist.
			flags := []string{"session-id", "agent-id", "result"}
			for _, flag := range flags {
				if sub.Flags().Lookup(flag) == nil {
					t.Errorf("session end missing flag: --%s", flag)
				}
			}
		}
	}
	if !found {
		t.Fatal("codero session end subcommand not registered")
	}
}
