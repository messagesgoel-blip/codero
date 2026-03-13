package redis

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestNew_ReturnsClient(t *testing.T) {
	c := New("localhost:6379", "")
	if c == nil {
		t.Fatal("New returned nil")
	}
	defer c.Close()
	if c.Unwrap() == nil {
		t.Error("Unwrap returned nil")
	}
}

func TestPing_SuccessWithMiniredis(t *testing.T) {
	mr := miniredis.RunT(t)
	c := New(mr.Addr(), "")
	defer c.Close()

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping against miniredis: %v", err)
	}
}

func TestPing_FailsOnUnreachableAddr(t *testing.T) {
	c := New("127.0.0.1:19998", "")
	defer c.Close()

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping to unreachable addr: expected error, got nil")
	}
}

func TestCheckHealth_FailsWithNamedError(t *testing.T) {
	// Reserve an ephemeral port, then close it to guarantee the address is not listening.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	err = CheckHealth(context.Background(), addr, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got: %v", err)
	}
}

func TestCheckHealth_SuccessWithMiniredis(t *testing.T) {
	mr := miniredis.RunT(t)
	err := CheckHealth(context.Background(), mr.Addr(), "")
	if err != nil {
		t.Fatalf("CheckHealth against miniredis: %v", err)
	}
}
