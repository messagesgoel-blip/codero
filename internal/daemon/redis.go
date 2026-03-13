package daemon

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/codero/codero/internal/redis"
)

// degraded is set to 1 when Redis connectivity is lost after startup.
var degraded atomic.Int32

// WatchRedis monitors Redis connectivity after startup.
// On loss: logs "redis lost, halting dispatch", sets the package-level degraded flag.
// Retries with exponential backoff (1s, 2s, 4s, 8s, cap 30s).
// On reconnect: logs "redis restored", clears the degraded flag.
// Runs as a goroutine — call go WatchRedis(ctx, client).
func WatchRedis(ctx context.Context, client *redis.Client) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}

		err := client.Ping(ctx)
		if err != nil {
			if degraded.CompareAndSwap(0, 1) {
				log.Println("redis lost, halting dispatch")
			}
			// Reconnect loop with exponential backoff.
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}

				if retryErr := client.Ping(ctx); retryErr == nil {
					degraded.Store(0)
					backoff = time.Second
					log.Println("redis restored")
					break
				}
			}
		}
	}
}

// IsDegraded reports whether the daemon is in a degraded state due to Redis loss.
func IsDegraded() bool {
	return degraded.Load() == 1
}
