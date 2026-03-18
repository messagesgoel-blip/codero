package idleprobe

import "testing"

func TestIntentionalFail(t *testing.T) {
	t.Fatal("intentional failure for CODERO-IDLE-002 audit")
}
