package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrRedisUnavailable is returned by CheckRedis when all retry attempts fail.
var ErrRedisUnavailable = errors.New("redis unavailable")

// degraded is set to 1 when Redis connectivity is lost after startup.
var degraded atomic.Int32

// CheckRedis attempts to PING the Redis server with the given options.
// Returns nil on success. Retries 3 times with 1-second backoff.
// Returns ErrRedisUnavailable if all attempts fail.
// The caller-provided context can be used to cancel the check.
func CheckRedis(ctx context.Context, opts *redis.Options) error {
	client := redis.NewClient(opts)
	defer client.Close()

	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("redis check cancelled: %w", ctx.Err())
			case <-time.After(time.Second):
			}
		}
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, lastErr = client.Ping(pingCtx).Result()
		cancel()
		if lastErr == nil {
			return nil
		}
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

		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := client.Ping(pingCtx).Result()
		cancel()

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

				retryCtx, retryCancel := context.WithTimeout(ctx, 3*time.Second)
				_, retryErr := client.Ping(retryCtx).Result()
				retryCancel()
				if retryErr == nil {
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
