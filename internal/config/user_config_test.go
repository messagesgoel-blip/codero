package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserConfigSave_WritesAtomically(t *testing.T) {
	t.Setenv("CODERO_USER_CONFIG_DIR", t.TempDir())

	uc := &UserConfig{
		Version:    1,
		DaemonAddr: "127.0.0.1:8110",
	}
	if err := uc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path, err := UserConfigPath()
	if err != nil {
		t.Fatalf("UserConfigPath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "daemon_addr: 127.0.0.1:8110") {
		t.Fatalf("saved config missing daemon_addr: %s", data)
	}

	tmpFiles, err := filepath.Glob(path + ".tmp-*")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(tmpFiles) != 0 {
		t.Fatalf("temporary files left behind: %v", tmpFiles)
	}
}
