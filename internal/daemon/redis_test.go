package daemon

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestCheckRedis_FailsWithNamedError(t *testing.T) {
	// Keep the listener open for the test duration so the port cannot be
	// recycled by a concurrently-started miniredis instance between close and
	// the CheckRedis call (TOCTOU fix). Each accepted connection is immediately
	// closed so go-redis sees EOF on every PING attempt, causing CheckRedis to
	// exhaust its retries and return ErrRedisUnavailable.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	addr := l.Addr().String()

	// Accept connections and immediately close them; goroutine exits when the
	// listener is closed by t.Cleanup.
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return // listener closed
			}
			conn.Close()
		}
	}()

	err = CheckRedis(context.Background(), addr, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRedisUnavailable) {
		t.Errorf("expected ErrRedisUnavailable, got: %v", err)
	}
}

func TestCheckRedis_SuccessWithMiniredis(t *testing.T) {
	mr := miniredis.RunT(t)
	err := CheckRedis(context.Background(), mr.Addr(), "")
	if err != nil {
		t.Fatalf("CheckRedis against miniredis: %v", err)
	}
}
