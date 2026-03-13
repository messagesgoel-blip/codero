package daemon

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/codero/codero/internal/log"
)

const gracePeriod = 30 * time.Second

// HandleSignals listens for SIGTERM and SIGINT.
// On receipt: logs "codero: shutting down", calls cancel() on the root context,
// waits for the provided WaitGroup, then returns the exit code.
// Grace period: 30 seconds. After the grace period, returns 1 with a log line.
// Returns 0 on graceful shutdown, 1 if grace period exceeded.
func HandleSignals(cancel context.CancelFunc, wg *sync.WaitGroup) int {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)

	<-ch
	log.Info("codero: shutting down",
		log.FieldEventType, log.EventShutdown,
		log.FieldComponent, "daemon",
	)
	cancel()

	// Use a context to signal completion and allow the goroutine to exit on timeout.
	ctx, waitCancel := context.WithCancel(context.Background())
	defer waitCancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Wait for either the wait group to finish or the timeout context to trigger.
		waitDone := make(chan struct{})
		go func() {
			wg.Wait()
			close(waitDone)
		}()

		select {
		case <-waitDone:
			// Graceful exit of this goroutine.
		case <-ctx.Done():
			// HandleSignals timed out.
		}
	}()

	select {
	case <-done:
		return 0
	case <-time.After(gracePeriod):
		log.Warn("codero: grace period exceeded, forcing exit",
			log.FieldEventType, log.EventShutdown,
			log.FieldComponent, "daemon",
		)
		return 1
	}
}

// SIGKILL recovery is handled at startup in P1-S1-07 (crash recovery).
// On unclean exit: the PID file may remain and lease keys in Redis
// may be inconsistent with SQLite state. Startup must audit both.
