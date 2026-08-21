package shared

import (
	"sync"
)

// AliasTable merges model aliases from two sources:
//   - plugin config (plugins.configs.<id>.model_aliases)
//   - host oauth-model-alias for this provider, relayed via auth.* requests
//
// Effective aliases are the union; on conflict the plugin-config entry wins.
type AliasTable struct {
	mu     sync.RWMutex
	config map[string]string // alias -> upstream
	host   map[string]string // alias -> upstream
}

// NewAliasTable creates an empty AliasTable.
func NewAliasTable() *AliasTable {
	return &AliasTable{}
}

func cleanAliases(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for alias, upstream := range in {
		if alias == "" || upstream == "" || alias == upstream {
			continue
		}
		out[alias] = upstream
	}
	return out
}

// SetConfig replaces the plugin-config alias source.
func (a *AliasTable) SetConfig(aliases map[string]string) {
	a.mu.Lock()
	a.config = cleanAliases(aliases)
	a.mu.Unlock()
}

// SetHost replaces the host-relayed (dashboard oauth-model-alias) source.
func (a *AliasTable) SetHost(aliases map[string]string) {
	a.mu.Lock()
	a.host = cleanAliases(aliases)
	a.mu.Unlock()
}

// Resolve returns the upstream ID for an alias, or the input unchanged when it
// is not an alias.
func (a *AliasTable) Resolve(modelID string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if up, ok := a.config[modelID]; ok {
		return up
	}
	if up, ok := a.host[modelID]; ok {
		return up
	}
	return modelID
}

// Entries returns the effective alias -> upstream map (config wins on conflict).
func (a *AliasTable) Entries() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[string]string, len(a.config)+len(a.host))
	for k, v := range a.host {
		out[k] = v
	}
	for k, v := range a.config {
		out[k] = v
	}
	return out
}
