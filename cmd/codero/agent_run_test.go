package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedHookScratchState(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	sessionID := "12345678-1234-1234-1234-123456789abc"
	secret := "hb-test-secret"

	if err := seedHookScratchState(sessionID, secret); err != nil {
		t.Fatalf("seedHookScratchState: %v", err)
	}

	dir := hookScratchDir(sessionID)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat scratch dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("scratch dir perms = %o, want 700", got)
	}

	for name, want := range map[string]string{
		"session-id": sessionID,
		"secret":     secret,
	} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got := string(data); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := fileInfo.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s perms = %o, want 600", name, got)
		}
	}
}

func TestParseGitDiffVolume_CountsBinaryChangesOncePerFile(t *testing.T) {
	out := []byte(strings.Join([]string{
		"10\t5\ttext.txt",
		"-\t-\tbinary.dat",
		"-\t12\tmixed.bin",
	}, "\n"))

	got := parseGitDiffVolume(out)
	const want int64 = 29
	if got != want {
		t.Fatalf("parseGitDiffVolume = %d, want %d", got, want)
	}
}
