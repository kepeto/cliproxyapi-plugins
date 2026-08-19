package shared

// ModelInfo builds the CPA model-info map for a single model entry.
func ModelInfo(id, name string) map[string]any {
	return map[string]any{
		"id":      id,
		"object":  "model",
		"owned_by": name,
		"name":    name,
	}
}

// ModelInfoWithContext builds the CPA model-info map with context window info.
func ModelInfoWithContext(id, name string, contextWindow, maxTokens int) map[string]any {
	m := ModelInfo(id, name)
	if contextWindow > 0 {
		m["context_window"] = contextWindow
	}
	if maxTokens > 0 {
		m["max_tokens"] = maxTokens
	}
	return m
}
