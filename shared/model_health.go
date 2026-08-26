package shared

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// ModelHealth tracks transient, per-model inference failures without mutating
// the provider's authoritative model catalog.
type ModelHealth struct {
	mu              sync.Mutex
	threshold       int
	cooldown        time.Duration
	maxCooldown     time.Duration
	failureWindow   time.Duration
	outageWindow    time.Duration
	outageThreshold int
	states          map[string]modelHealthState
	recentFailures  map[string]map[string]time.Time
}

type modelHealthState struct {
	failures       int
	lastFailure    time.Time
	quarantinedTil time.Time
	probing        bool
	quarantines    int
}

// NewModelHealth creates an in-memory model quarantine registry.
func NewModelHealth(threshold int, cooldown time.Duration) *ModelHealth {
	if threshold < 1 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 15 * time.Minute
	}
	return &ModelHealth{
		threshold:       threshold,
		cooldown:        cooldown,
		maxCooldown:     time.Hour,
		failureWindow:   15 * time.Minute,
		outageWindow:    5 * time.Minute,
		outageThreshold: 3,
		states:          make(map[string]modelHealthState),
		recentFailures:  make(map[string]map[string]time.Time),
	}
}

// Allow reports whether a request may use model. An expired quarantine permits
// one recovery probe; concurrent callers wait for that probe to finish.
func (h *ModelHealth) Allow(scope, model string) bool {
	key := healthKey(scope, model)
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.states[key]
	if !ok {
		return true
	}
	if now.Before(state.quarantinedTil) {
		return false
	}
	if !state.quarantinedTil.IsZero() {
		if state.probing {
			return false
		}
		state.probing = true
		h.states[key] = state
	}
	return true
}

// Hidden reports whether model should be omitted from a catalog projection.
func (h *ModelHealth) Hidden(scope, model string) bool {
	key := healthKey(scope, model)
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	state, ok := h.states[key]
	if !ok {
		return false
	}
	if now.Before(state.quarantinedTil) || state.probing {
		return true
	}
	return false
}

// Filter removes quarantined models while preserving the provider catalog.
func (h *ModelHealth) Filter(scope string, models []string) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		if !h.Hidden(scope, model) {
			out = append(out, model)
		}
	}
	return out
}

// RecordSuccess clears failures and releases a recovery probe.
func (h *ModelHealth) RecordSuccess(scope, model string) {
	key := healthKey(scope, model)
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.states, key)
	if failures := h.recentFailures[scope]; failures != nil {
		delete(failures, model)
	}
}

// RecordFailure records one logical request failure. It returns true when the
// model is quarantined by this failure. Generic provider-wide failures are not
// quarantined once several distinct models fail in the same short window.
func (h *ModelHealth) RecordFailure(scope, model string) bool {
	if model == "" {
		return false
	}
	now := time.Now()
	key := healthKey(scope, model)
	h.mu.Lock()
	defer h.mu.Unlock()

	recent := h.recentFailures[scope]
	if recent == nil {
		recent = make(map[string]time.Time)
		h.recentFailures[scope] = recent
	}
	for id, at := range recent {
		if now.Sub(at) > h.outageWindow {
			delete(recent, id)
		}
	}
	recent[model] = now
	providerOutage := len(recent) >= h.outageThreshold

	state := h.states[key]
	if !state.lastFailure.IsZero() && now.Sub(state.lastFailure) > h.failureWindow {
		state.failures = 0
		state.quarantines = 0
	}
	state.failures++
	state.lastFailure = now
	state.probing = false
	if state.failures >= h.threshold && !providerOutage {
		state.quarantines++
		cooldown := h.cooldown
		for i := 1; i < state.quarantines; i++ {
			cooldown *= 2
			if cooldown >= h.maxCooldown {
				cooldown = h.maxCooldown
				break
			}
		}
		state.quarantinedTil = now.Add(cooldown)
		h.states[key] = state
		return true
	}
	h.states[key] = state
	return false
}

func healthKey(scope, model string) string { return scope + "\x00" + model }

// IsModelSpecificFailure classifies only failures that can reasonably be
// attributed to a selected model. Authentication, rate limits, generic 5xx,
// and caller errors are intentionally excluded.
func IsModelSpecificFailure(status int, body []byte, err error) bool {
	if err != nil {
		return status == 408
	}
	if status == 408 {
		return true
	}
	if status == 401 || status == 403 || status == 429 || status >= 500 {
		return false
	}
	if status < 400 {
		return false
	}
	text := strings.ToLower(string(body))
	return strings.Contains(text, "model") && (strings.Contains(text, "unavailable") || strings.Contains(text, "not found") || strings.Contains(text, "does not exist") || strings.Contains(text, "invalid"))
}

// ValidChatResponse performs deliberately narrow validation for a non-stream
// OpenAI-compatible response. Reasoning/tool calls may have empty text content.
func ValidChatResponse(body []byte) bool {
	var response struct {
		Error   json.RawMessage   `json:"error"`
		Choices []json.RawMessage `json:"choices"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Error) > 0 || len(response.Choices) == 0 {
		return false
	}
	return true
}
