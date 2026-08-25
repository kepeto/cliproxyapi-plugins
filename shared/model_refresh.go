package shared

import (
	"sync"
	"time"
)

// ModelRefresher periodically refreshes a model list from upstream.
type ModelRefresher struct {
	mu            sync.RWMutex
	refreshMu     sync.Mutex
	models        []string
	lastRefresh   time.Time
	interval      time.Duration
	retryInterval time.Duration
	fetch         func() ([]string, error)
	healthCheck   func() bool
	stopCh        chan struct{}
}

// NewModelRefresher creates a refresher with the given interval and fetch function.
func NewModelRefresher(interval time.Duration, fetch func() ([]string, error), healthCheck func() bool) *ModelRefresher {
	if interval <= 0 {
		interval = 3 * time.Hour
	}
	retryInterval := time.Minute
	if interval < retryInterval {
		retryInterval = interval
	}
	return &ModelRefresher{
		interval:      interval,
		retryInterval: retryInterval,
		models:        make([]string, 0),
		fetch:         fetch,
		healthCheck:   healthCheck,
		stopCh:        make(chan struct{}),
	}
}

// Start begins the periodic refresh loop in a background goroutine.
// It performs an immediate first refresh, then repeats every interval.
func (r *ModelRefresher) Start() {
	go func() {
		_ = r.Refresh()
		ticker := time.NewTicker(r.nextInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = r.Refresh()
				ticker.Reset(r.nextInterval())
			case <-r.stopCh:
				return
			}
		}
	}()
}

// nextInterval retries empty catalogs quickly, then returns to the normal
// refresh interval after a successful catalog fetch. This avoids keeping an
// empty catalog for the full normal interval after boot or a transient outage.
func (r *ModelRefresher) nextInterval() time.Duration {
	r.mu.RLock()
	empty := len(r.models) == 0
	r.mu.RUnlock()
	if empty {
		return r.retryInterval
	}
	return r.interval
}

// Stop halts the background refresh loop.
func (r *ModelRefresher) Stop() {
	close(r.stopCh)
}

// Refresh performs a single catalog fetch and updates the model list.
func (r *ModelRefresher) Refresh() error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	return r.refresh()
}

// RefreshIfEmpty waits for any in-flight refresh and fetches only when no catalog is available.
func (r *ModelRefresher) RefreshIfEmpty() error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	r.mu.RLock()
	empty := len(r.models) == 0
	r.mu.RUnlock()
	if !empty {
		return nil
	}
	return r.refresh()
}

func (r *ModelRefresher) refresh() error {
	if r.fetch == nil {
		return nil
	}
	models, err := r.fetch()
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.models = models
	r.lastRefresh = time.Now()
	r.mu.Unlock()
	return nil
}

// Models returns a snapshot of the current model IDs.
func (r *ModelRefresher) Models() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.models) == 0 {
		return nil
	}
	out := make([]string, len(r.models))
	copy(out, r.models)
	return out
}

// LastRefresh returns when the catalog was last successfully refreshed.
func (r *ModelRefresher) LastRefresh() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastRefresh
}

// Healthy reports whether the upstream health check passes.
func (r *ModelRefresher) Healthy() bool {
	if r.healthCheck == nil {
		return true
	}
	return r.healthCheck()
}

// Contains reports whether the model ID is in the current snapshot.
func (r *ModelRefresher) Contains(modelID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.models {
		if m == modelID {
			return true
		}
	}
	return false
}
