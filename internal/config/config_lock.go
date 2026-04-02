package config

import "sync"

// ConfigMu serializes read-modify-write cycles on the per-user config file.
var ConfigMu sync.Mutex
