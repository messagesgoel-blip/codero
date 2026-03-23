package daemon

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	loglib "github.com/codero/codero/internal/log"
)

const gracePeriod = 30 * time.Second

// HandleSignals listens for SIGTERM and SIGINT.
// On receipt: logs "codero: shutting down", calls markNotReady (if non-nil)
// to immediately stop advertising readiness, calls cancel() on the root context,
// waits for the provided WaitGroup, then returns 0.
// Grace period: 30 seconds. After the grace period, returns 1 with a log line.
// The caller is responsible for exiting; returning (rather than calling os.Exit)
// allows deferred cleanup (e.g. PID file removal) to run.
func HandleSignals(cancel context.CancelFunc, wg *sync.WaitGroup, markNotReady func()) int {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(ch)

	<-ch
	loglib.Info("codero: shutting down",
		loglib.FieldEventType, loglib.EventShutdown,
		loglib.FieldComponent, "daemon",
	)

	// Immediately stop advertising readiness before waiting for goroutine drain.
	// This ensures /ready returns 503 during the grace window.
	if markNotReady != nil {
		markNotReady()
	}

	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return 0
	case <-time.After(gracePeriod):
		loglib.Warn("codero: grace period exceeded, forcing exit",
			loglib.FieldEventType, loglib.EventShutdown,
			loglib.FieldComponent, "daemon",
		)
		return 1
	}
}

// SIGKILL recovery is handled at startup in P1-S1-07 (crash recovery).
// On unclean exit: the PID file may remain and lease keys in Redis
// may be inconsistent with SQLite state. Startup must audit both.
