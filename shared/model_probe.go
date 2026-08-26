package shared

import (
	"sync"
	"time"
)

// ModelProbeTarget identifies a canonical model within one health scope.
type ModelProbeTarget struct {
	Scope string
	Model string
}

// ModelProbeOutcome controls how a completed probe updates model health.
type ModelProbeOutcome uint8

const (
	ProbeSucceeded ModelProbeOutcome = iota
	ProbeFailed
	ProbeIgnored
)

// ModelProbeFunc executes one provider-specific probe. It must not hold the
// ModelHealth mutex while doing network I/O.
type ModelProbeFunc func(ModelProbeTarget) ModelProbeOutcome

// ModelProbeScheduler periodically probes provider models and updates a shared
// ModelHealth registry. It has a terminal, idempotent lifecycle.
type ModelProbeScheduler struct {
	health    *ModelHealth
	interval  time.Duration
	targets   func() []ModelProbeTarget
	probe     ModelProbeFunc
	lifecycle sync.Mutex
	stopCh    chan struct{}
	doneCh    chan struct{}
	started   bool
	stopped   bool
}

// NewModelProbeScheduler creates a scheduler. A non-positive interval defaults
// to fifteen minutes, matching the initial model-health cooldown.
func NewModelProbeScheduler(interval time.Duration, health *ModelHealth, targets func() []ModelProbeTarget, probe ModelProbeFunc) *ModelProbeScheduler {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return &ModelProbeScheduler{
		health:   health,
		interval: interval,
		targets:  targets,
		probe:    probe,
		stopCh:   make(chan struct{}),
	}
}

// Start begins periodic probes after the first configured interval. Delaying
// the first pass avoids a startup request storm while catalog refreshes settle.
func (s *ModelProbeScheduler) Start() {
	s.lifecycle.Lock()
	if s.started || s.stopped {
		s.lifecycle.Unlock()
		return
	}
	s.started = true
	s.doneCh = make(chan struct{})
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.lifecycle.Unlock()

	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				s.runOnce(stopCh)
			}
		}
	}()
}

// Stop stops future probe passes and waits for the active pass to finish.
// Provider probe functions must use bounded network timeouts.
func (s *ModelProbeScheduler) Stop() {
	s.lifecycle.Lock()
	if s.stopped {
		s.lifecycle.Unlock()
		return
	}
	s.stopped = true
	if !s.started {
		s.lifecycle.Unlock()
		return
	}
	close(s.stopCh)
	doneCh := s.doneCh
	s.lifecycle.Unlock()
	<-doneCh
}

func (s *ModelProbeScheduler) runOnce(stopCh <-chan struct{}) {
	if s.health == nil || s.targets == nil || s.probe == nil {
		return
	}
	for _, target := range s.targets() {
		select {
		case <-stopCh:
			return
		default:
		}
		if !s.health.BeginProbe(target.Scope, target.Model) {
			continue
		}
		switch s.probe(target) {
		case ProbeSucceeded:
			s.health.RecordProbeSuccess(target.Scope, target.Model)
		case ProbeFailed:
			s.health.RecordProbeFailure(target.Scope, target.Model)
		default:
			s.health.RecordProbeIgnored(target.Scope, target.Model)
		}
	}
}
