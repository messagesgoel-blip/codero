package daemon

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	loglib "github.com/codero/codero/internal/log"
	redislib "github.com/codero/codero/internal/redis"
)

// ErrRedisUnavailable is returned by CheckRedis when all retry attempts fail.
var ErrRedisUnavailable = errors.New("redis unavailable")

// degraded is set to 1 when Redis connectivity is lost after startup.
var degraded atomic.Int32

// CheckRedis attempts to PING the configured Redis address.
// Returns nil on success. Retries 3 times with 1-second backoff.
// Returns ErrRedisUnavailable if all attempts fail.
func CheckRedis(ctx context.Context, addr, password string) error {
	if err := redislib.CheckHealth(ctx, addr, password); err != nil {
		return errors.Join(ErrRedisUnavailable, err)
	}
	return nil
}

// WatchRedis monitors Redis connectivity after startup.
// On loss: logs "redis lost, halting dispatch", sets the package-level degraded flag.
// Retries with exponential backoff (1s, 2s, 4s, 8s, cap 30s).
// On reconnect: logs "redis restored", clears the degraded flag.
// Runs as a goroutine — call go WatchRedis(ctx, client).
func WatchRedis(ctx context.Context, client *redislib.Client) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}

		if err := client.Ping(ctx); err != nil {
			if degraded.CompareAndSwap(0, 1) {
				loglib.Warn("redis lost, halting dispatch",
					loglib.FieldEventType, loglib.EventSystem,
					loglib.FieldComponent, "daemon",
				)
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

				if err := client.Ping(ctx); err == nil {
					degraded.Store(0)
					backoff = time.Second
					loglib.Info("redis restored",
						loglib.FieldEventType, loglib.EventSystem,
						loglib.FieldComponent, "daemon",
					)
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
