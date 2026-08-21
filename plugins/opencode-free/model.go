package main

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func handleModelStatic(rawReq []byte) ([]byte, error) {
	if err := keylessAuth.Ensure(rawReq); err != nil {
		return errorEnvelope("auth_bootstrap_failed", err.Error()), nil
	}
	if err := opencodeRefresher.RefreshIfEmpty(); err != nil && len(opencodeRefresher.Models()) == 0 {
		return errorEnvelope("model_refresh_failed", err.Error()), nil
	}
	models := make([]map[string]interface{}, 0)
	for _, id := range opencodeRefresher.Models() {
		models = append(models, modelEntry(id))
	}
	for alias := range modelAliases.Entries() {
		models = append(models, modelEntry(alias))
	}

	return shared.OKEnvelope(shared.MustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"Models":   models,
	}))
}

// modelEntry builds one catalog entry for a model ID (upstream or alias).
func modelEntry(id string) map[string]interface{} {
	return map[string]interface{}{
		"ID":                         prefixedModelID(id),
		"Object":                     "model",
		"Created":                    0,
		"OwnedBy":                    "opencode-free",
		"Type":                       PROVIDER_ID,
		"DisplayName":                id,
		"Name":                       id,
		"ContextLength":              128000,
		"MaxCompletionTokens":        4096,
		"SupportedGenerationMethods": []string{"chat"},
		"SupportedInputModalities":   []string{"text"},
		"SupportedOutputModalities":  []string{"text"},
		"UserDefined":                false,
	}
}

func handleModelForAuth(rawReq []byte) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}
	authID, _ := req["AuthID"].(string)
	if authID == "" {
		return errorEnvelope("invalid_request", "missing auth_id"), nil
	}

	alive := opencodeRefresher.Healthy()

	models := make([]map[string]interface{}, 0)
	catalogEntries := make([]map[string]interface{}, 0)
	for _, id := range opencodeRefresher.Models() {
		models = append(models, modelEntry(id))
		catalogEntries = append(catalogEntries, map[string]interface{}{
			"id":   id,
			"name": id,
		})
	}
	for alias := range modelAliases.Entries() {
		models = append(models, modelEntry(alias))
	}

	catalogJSON, _ := json.Marshal(catalogEntries)

	return shared.OKEnvelope(shared.MustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"AuthID":   authID,
		"AuthUpdate": map[string]interface{}{
			"model_catalog": base64.StdEncoding.EncodeToString(catalogJSON),
		},
		"Models": models,
		"Upstream": map[string]interface{}{
			"OpencodeAlive": alive,
			"CheckedAt":     time.Now().Format(time.RFC3339),
		},
	}))
}
