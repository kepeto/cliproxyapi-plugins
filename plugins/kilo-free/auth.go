package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// handleAuthParse recognizes kilo-free credential JSON files and returns the auth record.
func handleAuthParse(raw []byte) ([]byte, error) {
	// Parse the AuthParseRequest to extract the actual auth file content
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		return okEnvelopeJSON(`{"Handled":false}`)
	}

	// Extract RawJSON (the actual auth file content, base64-encoded in JSON)
	var authFileRaw []byte
	if rawJSON, ok := req["RawJSON"].(string); ok && rawJSON != "" {
		// RawJSON is base64-encoded in the JSON request
		decoded, err := base64.StdEncoding.DecodeString(rawJSON)
		if err == nil {
			authFileRaw = decoded
		} else {
			// If not valid base64, try as plain string
			authFileRaw = []byte(rawJSON)
		}
	} else if rawJSONBytes, ok := req["RawJSON"].([]byte); ok {
		authFileRaw = rawJSONBytes
	} else {
		// Fallback: try to get raw auth content from the request
		authFileRaw = raw
	}

	// Parse the actual auth file content
	var probe map[string]any
	if err := json.Unmarshal(authFileRaw, &probe); err != nil {
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
		"Label":       "KiloCode Free",
		"StorageJSON": authFileRaw,
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
