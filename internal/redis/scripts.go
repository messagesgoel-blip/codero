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
// Full implementations are added per-stage.
var placeholderScripts = map[string]string{
	ScriptSlotAcquire: `-- slot_acquire: atomically acquire a dispatch slot
-- KEYS[1] = lease key (codero:<repo>:lease:<branch>)
-- KEYS[2] = queue key (codero:<repo>:queue:pending)
-- ARGV[1] = holder ID
-- ARGV[2] = TTL in milliseconds
-- ARGV[3] = branch name
-- Returns: {1, expiry_ms} on success (acquired or extended own lease)
--          {0} if lease held by another holder
-- Note: redis.call aborts on Redis errors; script does not return {-1}

local lease_key = KEYS[1]
local queue_key = KEYS[2]
local holder_id = ARGV[1]
local ttl_ms = tonumber(ARGV[2])
local branch = ARGV[3]

-- Check if lease exists
local current = redis.call('GET', lease_key)
if current and current ~= holder_id then
    -- Lease held by another
    return {0}
end

-- Acquire or extend lease
redis.call('SET', lease_key, holder_id, 'PX', ttl_ms)

-- Remove from queue if present
redis.call('ZREM', queue_key, branch)

-- Return success with expiry timestamp
local expiry = redis.call('PEXPIRETIME', lease_key)
return {1, expiry}
`,
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
