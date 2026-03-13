package daemon

import (
	"context"
	"errors"
	"log"
	"sync/atomic"
	"time"

	"github.com/codero/codero/internal/redis"
)

// ErrRedisUnavailable is returned by CheckRedis when all retry attempts fail.
var ErrRedisUnavailable = errors.New("redis unavailable")

// degraded is set to 1 when Redis connectivity is lost after startup.
var degraded atomic.Int32

// CheckRedis attempts to PING the configured Redis address.
// Returns nil on success. Retries 3 times with 1-second backoff.
// Returns ErrRedisUnavailable if all attempts fail.
func CheckRedis(ctx context.Context, addr, password string) error {
	client := redis.New(addr, password)
	defer client.Close()

	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
		if err := client.Ping(ctx); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return errors.Join(ErrRedisUnavailable, lastErr)
}

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

		if err := client.Ping(ctx); err != nil {
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

				if err := client.Ping(ctx); err == nil {
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
