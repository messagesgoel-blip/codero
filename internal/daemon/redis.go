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

// CheckRedis attempts a single PING to the configured Redis address.
// Returns nil on success, ErrRedisUnavailable on failure.
// For startup retry with backoff, use CheckRedisWithRetry.
func CheckRedis(ctx context.Context, addr, password string) error {
	if err := redislib.CheckHealth(ctx, addr, password); err != nil {
		return errors.Join(ErrRedisUnavailable, err)
	}
	return nil
}

// CheckRedisWithRetry attempts to PING the configured Redis address with
// configurable retry count and backoff interval.
// Returns nil on success. Returns ErrRedisUnavailable if all attempts fail.
// Per spec §6.2: defaults are 3 retries with 1s backoff.
func CheckRedisWithRetry(ctx context.Context, addr, password string, maxRetries, retryIntervalSec int) error {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if retryIntervalSec <= 0 {
		retryIntervalSec = 1
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(retryIntervalSec) * time.Second):
			}
		}

		if err := redislib.CheckHealth(ctx, addr, password); err == nil {
			return nil
		}
	}
	return ErrRedisUnavailable
}

// WatchRedis monitors Redis connectivity after startup.
// On loss: logs "redis lost, halting dispatch", sets the package-level degraded flag.
// Retries with exponential backoff (1s, 2s, 4s, 8s, cap 30s).
// On reconnect: logs "redis restored", clears the degraded flag.
// Runs as a goroutine — call go WatchRedis(ctx, client).
func WatchRedis(ctx context.Context, client *redislib.Client) {
	WatchRedisWithInterval(ctx, client, 0)
}

// WatchRedisWithInterval monitors Redis connectivity with a configurable
// health check interval. Per spec §6.2: default is 30 seconds.
func WatchRedisWithInterval(ctx context.Context, client *redislib.Client, healthIntervalSec int) {
	if healthIntervalSec <= 0 {
		healthIntervalSec = 30
	}
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(healthIntervalSec) * time.Second):
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

// SetDegraded sets or clears the degraded flag. Called at startup when Redis
// is unavailable to enter polling-only mode.
func SetDegraded(d bool) {
	if d {
		degraded.Store(1)
	} else {
		degraded.Store(0)
	}
}
