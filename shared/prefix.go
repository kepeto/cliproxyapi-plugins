package shared

import (
	"encoding/json"
	"strings"
)

// Prefix manages model-id namespacing so identical model IDs from different
// providers remain distinct in CPA and are not round-robined across providers.
type Prefix struct {
	value string
}

// NewPrefix creates a Prefix with the given value.
func NewPrefix(value string) *Prefix {
	return &Prefix{value: value}
}

// Value returns the current prefix string.
func (p *Prefix) Value() string {
	return p.value
}

// Set updates the prefix value.
func (p *Prefix) Set(value string) {
	p.value = value
}

// Prefixed returns the routing key CPA sees: "<prefix>/<realModelID>".
func (p *Prefix) Prefixed(realID string) string {
	if p.value == "" || realID == "" {
		return realID
	}
	return p.value + "/" + realID
}

// Strip removes this prefix from a model id, if present.
func (p *Prefix) Strip(modelID string) string {
	prefix := p.value + "/"
	if strings.HasPrefix(modelID, prefix) {
		return strings.TrimPrefix(modelID, prefix)
	}
	return modelID
}

// StripFromPayload rewrites the "model" field inside an OpenAI-style JSON
// payload, removing this prefix so the upstream receives the bare id.
func (p *Prefix) StripFromPayload(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return payload
	}
	if m, ok := root["model"].(string); ok && m != "" {
		if stripped := p.Strip(m); stripped != m {
			root["model"] = stripped
			if out, err := json.Marshal(root); err == nil {
				return out
			}
		}
	}
	return payload
}
