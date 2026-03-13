package daemon

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestCheckRedis_FailsWithNamedError(t *testing.T) {
	// Reserve an ephemeral port, then close it to guarantee the address is not listening.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	err = CheckRedis(context.Background(), &redis.Options{Addr: addr})
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
