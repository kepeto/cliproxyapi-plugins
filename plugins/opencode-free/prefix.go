package main

import (
	"github.com/kepeto/cliproxyapi-plugins/shared"
)

// pluginPrefix namespaces this plugin's models so identical model IDs exposed by
// multiple providers (e.g. "tencent/hy3:free") remain distinct in CPA and are not
// round-robined across providers. CPA routes "<prefix>/<model>" to this plugin;
// the executor strips the prefix before calling the upstream endpoint.
// Defaults to the plugin identifier; can be overridden via plugin config.
var pluginPrefix = shared.NewPrefix(PROVIDER_ID)

// prefixedModelID returns the routing key CPA sees: "<prefix>/<realModelID>".
func prefixedModelID(realID string) string {
	return pluginPrefix.Prefixed(realID)
}

// stripModelPrefix removes this plugin's prefix from a model id, if present.
func stripModelPrefix(modelID string) string {
	return pluginPrefix.Strip(modelID)
}

// stripModelPrefixFromPayload rewrites the "model" field inside an OpenAI-style JSON
// payload, removing this prefix so the upstream receives the bare id.
func stripModelPrefixFromPayload(payload []byte) []byte {
	return pluginPrefix.StripFromPayload(payload)
}

// setPluginPrefix updates the runtime prefix from plugin config.
func setPluginPrefix(prefix string) {
	if prefix != "" {
		pluginPrefix.Set(prefix)
	}
}

// currentPrefix returns the active prefix string.
func currentPrefix() string {
	return pluginPrefix.Value()
}
