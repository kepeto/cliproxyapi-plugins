package main

import (
	"encoding/json"
	"strings"
)

// handleAuthParse recognizes opencode-free credential JSON files and returns the auth record.
func handleAuthParse(raw []byte) ([]byte, error) {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return okEnvelopeJSON(`{"Handled":false}`)
	}
	typ, _ := probe["type"].(string)
	if strings.ToLower(strings.TrimSpace(typ)) != PROVIDER_ID {
		return okEnvelopeJSON(`{"Handled":false}`)
	}

	metadata := map[string]any{
		"type":     PROVIDER_ID,
		"username": PROVIDER_ID,
	}
	if v, ok := probe["access_token"].(string); ok && strings.TrimSpace(v) != "" {
		metadata["access_token"] = strings.TrimSpace(v)
	}
	if v, ok := probe["refresh_token"].(string); ok && strings.TrimSpace(v) != "" {
		metadata["refresh_token"] = strings.TrimSpace(v)
	}

	auth := map[string]any{
		"Provider":    PROVIDER_ID,
		"ID":          PROVIDER_ID,
		"FileName":    PROVIDER_ID + ".json",
		"Label":       "OpenCode Zen Free",
		"StorageJSON": raw,
		"Metadata":    metadata,
		"Attributes": map[string]string{
			"source":   "plugin:" + PROVIDER_ID,
			"provider": PROVIDER_ID,
		},
	}
	return okEnvelopeJSON(mustJSON(map[string]any{
		"Handled": true,
		"Auth":    auth,
	}))
}
