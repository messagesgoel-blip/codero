package redis

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func TestScriptRegistry_KnownNames(t *testing.T) {
	known := []string{
		ScriptSlotAcquire,
		ScriptSeqIncrement,
		ScriptPrecommitSlot,
		ScriptDailyCap,
	}
	for _, name := range known {
		if _, ok := placeholderScripts[name]; !ok {
			t.Errorf("placeholderScripts missing entry for constant %q", name)
		}
	}
}

func TestScriptRegistry_LoadAndLookup(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rc.Close()

	reg := NewScriptRegistry()
	if err := reg.Load(context.Background(), rc); err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, name := range []string{ScriptSlotAcquire, ScriptSeqIncrement, ScriptPrecommitSlot, ScriptDailyCap} {
		sha, err := reg.SHA(name)
		if err != nil {
			t.Errorf("SHA(%q): unexpected error: %v", name, err)
			continue
		}
		if sha == "" {
			t.Errorf("SHA(%q): got empty string", name)
		}
	}
}

func TestScriptRegistry_UnknownName(t *testing.T) {
	mr := miniredis.RunT(t)
	rc := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer rc.Close()

	reg := NewScriptRegistry()
	if err := reg.Load(context.Background(), rc); err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err := reg.SHA("does_not_exist")
	if err == nil {
		t.Error("SHA for unknown name: expected error, got nil")
	}
}

func TestScriptRegistry_SHABeforeLoad(t *testing.T) {
	reg := NewScriptRegistry()
	_, err := reg.SHA(ScriptSlotAcquire)
	if err == nil {
		t.Error("SHA before Load: expected error, got nil")
	}
}
