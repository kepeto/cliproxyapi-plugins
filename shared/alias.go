package shared

import "sync"

// AliasTable maps client-visible alias model IDs to upstream model IDs, so
// upstream renames (e.g. ox-alpha -> x-preview-f-free) stay hidden from
// clients. Configured via plugin config "model_aliases": {alias: upstream}.
type AliasTable struct {
	mu      sync.RWMutex
	aliases map[string]string // alias -> upstream
}

// NewAliasTable creates an empty AliasTable.
func NewAliasTable() *AliasTable {
	return &AliasTable{}
}

// Set replaces the alias map. Empty entries and self-mappings are dropped.
func (a *AliasTable) Set(aliases map[string]string) {
	clean := make(map[string]string, len(aliases))
	for alias, upstream := range aliases {
		if alias == "" || upstream == "" || alias == upstream {
			continue
		}
		clean[alias] = upstream
	}
	a.mu.Lock()
	a.aliases = clean
	a.mu.Unlock()
}

// Resolve returns the upstream ID for an alias, or the input unchanged when it
// is not an alias.
func (a *AliasTable) Resolve(modelID string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if up, ok := a.aliases[modelID]; ok {
		return up
	}
	return modelID
}

// Entries returns a copy of the current alias -> upstream map.
func (a *AliasTable) Entries() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[string]string, len(a.aliases))
	for k, v := range a.aliases {
		out[k] = v
	}
	return out
}
