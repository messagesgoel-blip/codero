package main

import (
	"strings"
	"testing"
)

func TestSubmitCmd_NoSessionID(t *testing.T) {
	t.Setenv("CODERO_SESSION_ID", "")
	t.Setenv("CODERO_AGENT_SESSION_ID", "")
	configPath := "nonexistent.yaml"
	cmd := submitCmd(&configPath)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no session ID")
	}
	if !strings.Contains(err.Error(), "CODERO_SESSION_ID") {
		t.Errorf("error should mention CODERO_SESSION_ID, got: %s", err.Error())
	}
}

func TestSubmitCmd_HelpOutput(t *testing.T) {
	configPath := "nonexistent.yaml"
	cmd := submitCmd(&configPath)
	cmd.SetArgs([]string{"--help"})

	var buf strings.Builder
	cmd.SetOut(&buf)
	_ = cmd.Execute()

	out := buf.String()
	if !strings.Contains(out, "--summary") {
		t.Errorf("help should mention --summary, got:\n%s", out)
	}
	if !strings.Contains(out, "--files") {
		t.Errorf("help should mention --files, got:\n%s", out)
	}
}

func TestSubmitCmd_FlagParsing(t *testing.T) {
	configPath := "nonexistent.yaml"
	cmd := submitCmd(&configPath)

	if cmd.Flags().Lookup("summary") == nil {
		t.Error("expected --summary flag to be registered")
	}
	if cmd.Flags().Lookup("files") == nil {
		t.Error("expected --files flag to be registered")
	}
}
