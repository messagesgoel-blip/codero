package daemon

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// freeAddr binds an ephemeral TCP port and immediately releases it,
// returning an address that is guaranteed not to be listening.
// Using :0 avoids the flakiness of hard-coding a port that may be in use.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeAddr: listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestCheckRedis_FailsWithNamedError(t *testing.T) {
	addr := freeAddr(t)
	err := CheckRedis(context.Background(), &redis.Options{Addr: addr})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRedisUnavailable) {
		t.Errorf("expected ErrRedisUnavailable, got: %v", err)
	}
}

func TestCheckRedis_SuccessWithMiniredis(t *testing.T) {
	mr := miniredis.RunT(t)
	err := CheckRedis(context.Background(), &redis.Options{Addr: mr.Addr()})
	if err != nil {
		t.Fatalf("CheckRedis against miniredis: %v", err)
	}
}
