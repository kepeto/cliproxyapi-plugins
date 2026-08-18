package main

import (
	"encoding/json"
	"strings"
)

// pluginPrefix namespaces this plugin's models so identical model IDs exposed by
// multiple providers (e.g. "tencent/hy3:free") remain distinct in CPA and are not
// round-robined across providers. CPA routes "<prefix>/<model>" to this plugin;
// the executor strips the prefix before calling the upstream endpoint.
// Defaults to the plugin identifier.
const pluginPrefix = "nous-portal-free"

// prefixedModelID returns the routing key CPA sees: "<prefix>/<realModelID>".
func prefixedModelID(realID string) string {
	if pluginPrefix == "" || realID == "" {
		return realID
	}
	return pluginPrefix + "/" + realID
}

// stripModelPrefix removes this plugin's prefix from a model id, if present.
func stripModelPrefix(modelID string) string {
	prefix := pluginPrefix + "/"
	if strings.HasPrefix(modelID, prefix) {
		return strings.TrimPrefix(modelID, prefix)
	}
	return modelID
}

// stripModelPrefixFromPayload rewrites the "model" field inside an OpenAI-style JSON
// payload, removing this plugin's prefix so the upstream receives the bare id.
func stripModelPrefixFromPayload(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return payload
	}
	if m, ok := root["model"].(string); ok && m != "" {
		if stripped := stripModelPrefix(m); stripped != m {
			root["model"] = stripped
			if out, err := json.Marshal(root); err == nil {
				return out
			}
		}
	}
	return payload
}
