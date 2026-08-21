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

// resolveModel strips the plugin prefix, then maps any configured alias to its
// upstream model ID.
func resolveModel(modelID string) string {
	return modelAliases.Resolve(stripModelPrefix(modelID))
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
