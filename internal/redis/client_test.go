package redis

import (
	"context"
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
