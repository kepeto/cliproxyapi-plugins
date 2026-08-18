package main

import (
	"encoding/json"
	"strings"
	"fmt"
	"os"
)

// handleAuthParse recognizes opencode-free credential JSON files and returns the auth record.
func handleAuthParse(raw []byte) ([]byte, error) {
	fmt.Fprintf(os.Stderr, "opencode-free auth.parse: len=%d raw=%s\n", len(raw), string(raw[:100]))
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return okEnvelopeJSON(`{"Handled":false}`)
	}
	typ, _ := probe["type"].(string)
	if strings.ToLower(strings.TrimSpace(typ)) != PROVIDER_ID {
		return okEnvelopeJSON(`{"Handled":false}`)
	}

	// Return dummy auth - opencode-free is keyless, executor bypasses auth
	auth := map[string]any{
		"Provider":    PROVIDER_ID,
		"ID":          PROVIDER_ID,
		"FileName":    PROVIDER_ID + ".json",
		"Label":       "OpenCode Zen Free",
		"StorageJSON": raw,
		"Metadata": map[string]any{
			"type":     PROVIDER_ID,
			"username": PROVIDER_ID,
		},
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
