package shared

import (
	"encoding/json"
)

// ConfigBytesFromLifecycle extracts the config payload from a
// plugin.register / plugin.reconfigure request. The host wraps it as
// {"config_yaml": <base64>, "schema_version": N}; bare config bytes are
// passed through unchanged.
func ConfigBytesFromLifecycle(raw []byte) []byte {
	var req struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	if err := json.Unmarshal(raw, &req); err == nil && len(req.ConfigYAML) > 0 {
		return req.ConfigYAML
	}
	return raw
}
