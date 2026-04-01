package deliverypipeline

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	loglib "github.com/codero/codero/internal/log"
)

const defaultNotificationTimeout = 5 * time.Second

// Notify dispatches the worktree notification hook. Errors are logged and ignored.
func Notify(worktree, notificationType, assignmentID string) {
	if strings.TrimSpace(worktree) == "" {
		return
	}
	hook := filepath.Join(worktree, coderoDir, "hooks", "on-feedback")
	if info, err := os.Stat(hook); err == nil && info.Mode()&0o111 != 0 {
		timeout := notificationTimeout()
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		cmd := exec.Command(hook, worktree, notificationType, assignmentID)
		cmd.Dir = worktree
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Start(); err != nil {
			loglib.Warn("delivery pipeline: notification hook failed to start",
				loglib.FieldComponent, "delivery_pipeline",
				"hook", hook,
				"error", err.Error(),
			)
			return
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case err := <-done:
			if err != nil {
				loglib.Warn("delivery pipeline: notification hook failed",
					loglib.FieldComponent, "delivery_pipeline",
					"hook", hook,
					"error", err.Error(),
					"output", strings.TrimSpace(out.String()),
				)
			}
		case <-time.After(timeout):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
			loglib.Warn("delivery pipeline: notification hook timed out",
				loglib.FieldComponent, "delivery_pipeline",
				"hook", hook,
				"timeout", timeout.String(),
			)
		}
		return
	}

	pending := filepath.Join(worktree, coderoDir, feedbackDirName, "pending")
	if err := writeAtomic(pending, []byte(time.Now().UTC().Format(time.RFC3339)), 0o644); err != nil {
		loglib.Warn("delivery pipeline: pending marker write failed",
			loglib.FieldComponent, "delivery_pipeline",
			"error", err.Error(),
			"path", pending,
		)
	}
}

func notificationTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CODERO_NOTIFICATION_HOOK_TIMEOUT"))
	if raw == "" {
		return defaultNotificationTimeout
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return defaultNotificationTimeout
		}
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	loglib.Warn("delivery pipeline: invalid CODERO_NOTIFICATION_HOOK_TIMEOUT, using default",
		loglib.FieldComponent, "delivery_pipeline",
		"value", raw,
	)
	return defaultNotificationTimeout
}
