package daemon

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
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
	defer signal.Stop(ch)

	<-ch
	log.Println("codero: shutting down")
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
		log.Println("codero: grace period exceeded, forcing exit")
		return 1
	}
}

// SIGKILL recovery is handled at startup in P1-S1-07 (crash recovery).
// On unclean exit: the PID file may remain and lease keys in Redis
// may be inconsistent with SQLite state. Startup must audit both.
