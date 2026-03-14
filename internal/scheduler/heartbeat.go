package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/codero/codero/internal/redis"
)

// HeartbeatConfig configures heartbeat behavior.
type HeartbeatConfig struct {
	Interval  time.Duration // how often to send heartbeat
	LeaseTTL  time.Duration // lease duration after each heartbeat
	MaxMisses int           // max consecutive misses before lease is considered lost
}

// DefaultHeartbeatConfig returns sensible defaults.
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		Interval:  10 * time.Second,
		LeaseTTL:  30 * time.Second,
		MaxMisses: 3,
	}
}

// Heartbeat maintains a lease through periodic renewal.
// It automatically releases the lease on Stop or context cancellation.
type Heartbeat struct {
	mu      sync.Mutex
	lm      *LeaseManager
	lease   *Lease
	config  HeartbeatConfig
	stopCh  chan struct{}
	doneCh  chan struct{}
	stopped bool
	misses  int
	lastErr error
}

// ErrHeartbeatStopped is returned when operations are attempted on a stopped heartbeat.
var ErrHeartbeatStopped = errors.New("heartbeat stopped")

// StartHeartbeat begins periodic lease renewal.
// The heartbeat continues until Stop is called or the context is cancelled.
// On context cancellation, the lease is automatically released.
func (lm *LeaseManager) StartHeartbeat(ctx context.Context, lease *Lease, cfg HeartbeatConfig) *Heartbeat {
	hb := &Heartbeat{
		lm:     lm,
		lease:  lease,
		config: cfg,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}

	go hb.run(ctx)

	return hb
}

func (hb *Heartbeat) run(ctx context.Context) {
	defer close(hb.doneCh)

	ticker := time.NewTicker(hb.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled - release lease and exit.
			hb.releaseAndStop()
			return
		case <-hb.stopCh:
			// Stop requested - release lease and exit.
			hb.releaseAndStop()
			return
		case <-ticker.C:
			// Time to extend the lease.
			newLease, err := hb.lm.Extend(ctx, hb.lease.Repo, hb.lease.Branch, hb.lease.HolderID, hb.config.LeaseTTL)
			if err != nil {
				hb.mu.Lock()
				hb.misses++
				hb.lastErr = err
				if hb.misses >= hb.config.MaxMisses {
					hb.mu.Unlock()
					// Too many missed heartbeats - release lease and mark stopped.
					hb.releaseAndStop()
					return
				}
				hb.mu.Unlock()
				continue
			}

			hb.mu.Lock()
			hb.misses = 0
			hb.lease = newLease
			hb.mu.Unlock()
		}
	}
}

// releaseAndStop releases the lease and marks the heartbeat as stopped.
// Must be called from the run goroutine when exiting.
func (hb *Heartbeat) releaseAndStop() {
	// Release the lease (best effort, ignore errors).
	_ = hb.lm.Release(context.Background(), hb.lease.Repo, hb.lease.Branch, hb.lease.HolderID)

	// Mark stopped and clear lease under lock to prevent stale Lease() output.
	hb.mu.Lock()
	hb.stopped = true
	hb.lease = nil
	hb.mu.Unlock()
}

// Stop stops the heartbeat and releases the lease.
// Safe to call multiple times.
func (hb *Heartbeat) Stop() {
	hb.mu.Lock()
	if hb.stopped {
		hb.mu.Unlock()
		return
	}
	hb.stopped = true
	hb.mu.Unlock()

	close(hb.stopCh)
	<-hb.doneCh // wait for goroutine to finish
}

// Lease returns the current lease state.
// Returns nil if the heartbeat has stopped.
func (hb *Heartbeat) Lease() *Lease {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if hb.stopped {
		return nil
	}
	return hb.lease
}

// Status returns the current heartbeat status.
func (hb *Heartbeat) Status() HeartbeatStatus {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	return HeartbeatStatus{
		Repo:      hb.lease.Repo,
		Branch:    hb.lease.Branch,
		HolderID:  hb.lease.HolderID,
		Misses:    hb.misses,
		LastErr:   hb.lastErr,
		ExpiresAt: hb.lease.ExpiresAt,
	}
}

// HeartbeatStatus represents the current state of a heartbeat.
type HeartbeatStatus struct {
	Repo      string
	Branch    string
	HolderID  string
	Misses    int
	LastErr   error
	ExpiresAt time.Time
}

// IsHealthy returns true if the heartbeat is active with no missed beats.
func (s HeartbeatStatus) IsHealthy() bool {
	return s.Misses == 0 && s.LastErr == nil
}

// IsExpired returns true if the lease has expired.
func (s HeartbeatStatus) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// MonitorHeartbeat watches for lease expiry without renewal.
// Use this for observing lease health without modifying it.
type MonitorHeartbeat struct {
	client *redis.Client
}

// NewMonitorHeartbeat creates a lease monitor.
func NewMonitorHeartbeat(client *redis.Client) *MonitorHeartbeat {
	return &MonitorHeartbeat{client: client}
}

// IsAlive checks if a lease exists and has not expired.
func (m *MonitorHeartbeat) IsAlive(ctx context.Context, repo, branch string) (bool, error) {
	key, err := redis.BuildKey(repo, "lease", branch)
	if err != nil {
		return false, fmt.Errorf("check lease alive: %w", err)
	}

	rc := m.client.Unwrap()
	ttl, err := rc.TTL(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("check lease alive: redis error: %w", err)
	}

	// TTL -2 means key doesn't exist, -1 means no expiry.
	// A positive TTL means the lease exists and is alive.
	if ttl < 0 {
		return false, nil
	}
	return ttl > 0, nil
}
