package daemon

import (
	"context"
	"time"

	"github.com/codero/codero/internal/config"
	loglib "github.com/codero/codero/internal/log"
)

// SyncAgentRegistry periodically refreshes the Codero-managed agent registry in
// the per-user config. It runs once immediately, then on the configured
// interval. Errors are logged and do not stop the daemon.
func SyncAgentRegistry(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = config.DefaultAgentRegistryScanInterval
	}

	refresh := func() {
		config.ConfigMu.Lock()
		defer config.ConfigMu.Unlock()

		uc, err := config.LoadUserConfig()
		if err != nil {
			loglib.Warn("codero: agent registry load failed",
				loglib.FieldComponent, "daemon",
				"error", err,
			)
			return
		}
		agents, err := uc.RefreshAgentRegistry(time.Now().UTC())
		if err != nil {
			loglib.Warn("codero: agent registry refresh failed",
				loglib.FieldComponent, "daemon",
				"error", err,
			)
			return
		}
		if err := uc.Save(); err != nil {
			loglib.Warn("codero: agent registry save failed",
				loglib.FieldComponent, "daemon",
				"error", err,
			)
			return
		}
		loglib.Info("codero: agent registry refreshed",
			loglib.FieldComponent, "daemon",
			"agents", len(agents),
			"interval", interval.String(),
		)
	}

	refresh()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}
