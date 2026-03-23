package daemon

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

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

func TestCheckRedisWithRetry_Defaults(t *testing.T) {
	// Test that zero values fall back to defaults
	mr := miniredis.RunT(t)
	err := CheckRedisWithRetry(context.Background(), mr.Addr(), "", 0, 0)
	if err != nil {
		t.Fatalf("CheckRedisWithRetry with zero values should use defaults: %v", err)
	}
}

func TestCheckRedisWithRetry_CustomRetries(t *testing.T) {
	// Test custom retry count with a failing address
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	addr := l.Addr().String()

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	err = CheckRedisWithRetry(context.Background(), addr, "", 2, 0)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRedisUnavailable) {
		t.Errorf("expected ErrRedisUnavailable, got: %v", err)
	}
}

func TestCheckRedisWithRetry_SuccessWithMiniredis(t *testing.T) {
	mr := miniredis.RunT(t)
	err := CheckRedisWithRetry(context.Background(), mr.Addr(), "", 3, 1)
	if err != nil {
		t.Fatalf("CheckRedisWithRetry against miniredis: %v", err)
	}
}

func TestCheckRedisWithRetry_ContextCancel(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	addr := l.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err = CheckRedisWithRetry(ctx, addr, "", 3, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
