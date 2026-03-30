package sessmetrics

import (
	"context"
	"time"

	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// Monitor runs the LiteLLM syncer and pressure detector on a fixed interval.
type Monitor struct {
	syncer   *LiteLLMSyncer
	detector *PressureDetector
	interval time.Duration
}

// NewMonitor returns a Monitor. If pgDSN is empty the syncer step is skipped
// (pressure detection still runs on any manually-inserted rows).
func NewMonitor(pgDSN string, db *state.DB, interval time.Duration) *Monitor {
	m := &Monitor{
		detector: NewPressureDetector(db),
		interval: interval,
	}
	if pgDSN != "" {
		m.syncer = NewLiteLLMSyncer(pgDSN, db)
	}
	return m
}

// Run blocks until ctx is cancelled, running sync+evaluate on each tick.
func (m *Monitor) Run(ctx context.Context) {
	if m.interval <= 0 {
		m.interval = 30 * time.Second
	}
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Run once immediately on startup.
	m.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runOnce(ctx)
		}
	}
}

func (m *Monitor) runOnce(ctx context.Context) {
	timeout := m.interval
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if m.syncer != nil {
		n, err := m.syncer.Sync(tctx)
		if err != nil {
			loglib.Warn("sessmetrics: litellm sync error",
				loglib.FieldComponent, "sessmetrics",
				"error", err,
			)
		} else if n > 0 {
			loglib.Info("sessmetrics: litellm sync complete",
				loglib.FieldComponent, "sessmetrics",
				"imported", n,
			)
		}
	}

	if err := m.detector.EvaluateAll(tctx); err != nil {
		loglib.Warn("sessmetrics: pressure evaluation error",
			loglib.FieldComponent, "sessmetrics",
			"error", err,
		)
	}
}
