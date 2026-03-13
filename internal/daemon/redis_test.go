package daemon

import (
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestCheckRedis_FailsWithNamedError(t *testing.T) {
	// Point at a port that is not listening.
	err := CheckRedis("127.0.0.1:19999")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRedisUnavailable) {
		t.Errorf("expected ErrRedisUnavailable, got: %v", err)
	}
}

func TestCheckRedis_SuccessWithMiniredis(t *testing.T) {
	mr := miniredis.RunT(t)
	err := CheckRedis(mr.Addr())
	if err != nil {
		t.Fatalf("CheckRedis against miniredis: %v", err)
	}
}
