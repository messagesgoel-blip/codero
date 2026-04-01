package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncAgentRegistry_ReturnsWhenContextAlreadyCanceled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODERO_USER_CONFIG_DIR", dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	SyncAgentRegistry(ctx, time.Hour)

	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("config.yaml should not be written when context is already canceled, err=%v", err)
	}
}
