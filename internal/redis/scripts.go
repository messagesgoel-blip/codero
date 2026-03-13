package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

// Script names — use these constants everywhere; never hardcode the string.
const (
	// ScriptSlotAcquire atomically acquires a dispatch slot.
	// Full logic implemented in P1-S3.
	ScriptSlotAcquire = "slot_acquire"

	// ScriptSeqIncrement atomically increments the inbox sequence counter.
	// Full logic implemented in P1-S4.
	ScriptSeqIncrement = "seq_increment"

	// ScriptPrecommitSlot acquires a rate-limited precommit review slot.
	// Full logic implemented in P1-S5.5.
	ScriptPrecommitSlot = "precommit_slot_acquire"

	// ScriptDailyCap checks and increments the daily precommit review counter.
	// Full logic implemented in P1-S5.5.
	ScriptDailyCap = "daily_cap_check"
)

// placeholderScripts maps each script name to its Lua source.
// All bodies are stubs returning nil; full logic is added per-stage.
var placeholderScripts = map[string]string{
	ScriptSlotAcquire:   `-- slot_acquire: placeholder (P1-S3)` + "\nreturn nil",
	ScriptSeqIncrement:  `-- seq_increment: placeholder (P1-S4)` + "\nreturn nil",
	ScriptPrecommitSlot: `-- precommit_slot_acquire: placeholder (P1-S5.5)` + "\nreturn nil",
	ScriptDailyCap:      `-- daily_cap_check: placeholder (P1-S5.5)` + "\nreturn nil",
}

// ScriptRegistry holds compiled SHA references for all Lua scripts.
// Load must be called once after the Redis connection is established.
type ScriptRegistry struct {
	shas map[string]string // name → SHA1 from SCRIPT LOAD
}

// Load registers all placeholder scripts on the Redis server and caches their SHAs.
// Must be called before any script is Run.
func (r *ScriptRegistry) Load(ctx context.Context, rc *goredis.Client) error {
	r.shas = make(map[string]string, len(placeholderScripts))
	for name, src := range placeholderScripts {
		sha, err := rc.ScriptLoad(ctx, src).Result()
		if err != nil {
			return fmt.Errorf("redis: load script %q: %w", name, err)
		}
		r.shas[name] = sha
	}
	return nil
}

// SHA returns the cached SHA for the named script.
// Returns an error if the script is unknown or Load has not been called.
func (r *ScriptRegistry) SHA(name string) (string, error) {
	if r.shas == nil {
		return "", fmt.Errorf("redis: script registry not loaded (call Load first)")
	}
	sha, ok := r.shas[name]
	if !ok {
		return "", fmt.Errorf("redis: unknown script %q", name)
	}
	return sha, nil
}

// NewScriptRegistry returns an empty, unloaded ScriptRegistry.
func NewScriptRegistry() *ScriptRegistry {
	return &ScriptRegistry{}
}
