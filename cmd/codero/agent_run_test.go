package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	daemongrpc "github.com/codero/codero/internal/daemon/grpc"
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

func TestCollectGitActivitySnapshot_NoHeadIncludesUntrackedFiles(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write new.txt: %v", err)
	}

	statuses, diffSize, err := collectGitActivitySnapshot(context.Background(), dir)
	if err != nil {
		t.Fatalf("collectGitActivitySnapshot: %v", err)
	}
	if statuses["new.txt"] != "??" {
		t.Fatalf("status for new.txt = %q, want ??", statuses["new.txt"])
	}
	if diffSize != 2 {
		t.Fatalf("diffSize = %d, want 2", diffSize)
	}
}

func TestCollectGitActivitySnapshot_WithHeadIncludesUntrackedFiles(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatalf("write tracked.txt: %v", err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "initial")

	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write fresh.txt: %v", err)
	}

	statuses, diffSize, err := collectGitActivitySnapshot(context.Background(), dir)
	if err != nil {
		t.Fatalf("collectGitActivitySnapshot: %v", err)
	}
	if statuses["fresh.txt"] != "??" {
		t.Fatalf("status for fresh.txt = %q, want ??", statuses["fresh.txt"])
	}
	if diffSize != 3 {
		t.Fatalf("diffSize = %d, want 3", diffSize)
	}
}

func TestSendHeartbeatSample_UsesTelemetryHeartbeatWhenCountersExist(t *testing.T) {
	client := &fakeHeartbeatClient{}
	tracker := newActivityTracker("")
	tracker.recordOutput([]byte("hello\nworld\n"))
	tracker.recordProcEvent()

	if err := sendHeartbeatSample(context.Background(), client, "sess-1", "hb-secret", tracker); err != nil {
		t.Fatalf("sendHeartbeatSample: %v", err)
	}
	if client.heartbeatCalls != 0 {
		t.Fatalf("heartbeatCalls = %d, want 0", client.heartbeatCalls)
	}
	if client.heartbeatWithContextCalls != 1 {
		t.Fatalf("heartbeatWithContextCalls = %d, want 1", client.heartbeatWithContextCalls)
	}
	if !client.lastMarkProgress {
		t.Fatal("lastMarkProgress = false, want true")
	}
	if client.lastContext.RuntimeBytes == 0 || client.lastContext.OutputLines != 2 || client.lastContext.ProcEvents != 1 {
		t.Fatalf("unexpected heartbeat context: %+v", client.lastContext)
	}
}

type fakeHeartbeatClient struct {
	heartbeatCalls            int
	heartbeatWithContextCalls int
	lastMarkProgress          bool
	lastContext               daemongrpc.HeartbeatContext
}

func (f *fakeHeartbeatClient) Heartbeat(_ context.Context, _, _ string, markProgress bool) error {
	f.heartbeatCalls++
	f.lastMarkProgress = markProgress
	return nil
}

func (f *fakeHeartbeatClient) HeartbeatWithContext(_ context.Context, _, _ string, markProgress bool, hctx daemongrpc.HeartbeatContext) error {
	f.heartbeatWithContextCalls++
	f.lastMarkProgress = markProgress
	f.lastContext = hctx
	return nil
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
