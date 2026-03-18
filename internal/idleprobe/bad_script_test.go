package idleprobe

import "testing"

func TestIntentionalFail(t *testing.T) {
	t.Fatal("intentional fail for TEST-1")
}
