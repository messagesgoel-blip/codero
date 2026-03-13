package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Client wraps go-redis and is the only permitted Redis client in codero.
// All callers must obtain a Client via New; raw go-redis clients must not
// be created outside this package.
type Client struct {
	rc *goredis.Client
}

// New creates a Client connected to addr with optional password.
// The connection is not verified until Ping or the first command.
func New(addr, password string) *Client {
	return &Client{
		rc: goredis.NewClient(&goredis.Options{
			Addr:     addr,
			Password: password,
		}),
	}
}

// Ping sends a PING to Redis and returns an error if it fails.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := c.rc.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	return nil
}

// Close releases the underlying connection pool.
func (c *Client) Close() error {
	return c.rc.Close()
}

// Unwrap returns the underlying *goredis.Client.
// Use only where the internal/redis API does not yet cover the required operation.
// Every direct use is a temporary bridge to be replaced as the API grows.
func (c *Client) Unwrap() *goredis.Client {
	return c.rc
}
