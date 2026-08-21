package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func handleModelStatic(rawReq []byte) ([]byte, error) {
	if err := keylessAuth.Ensure(rawReq); err != nil {
		return errorEnvelope("auth_bootstrap_failed", err.Error()), nil
	}
	if err := kiloRefresher.RefreshIfEmpty(); err != nil && len(kiloRefresher.Models()) == 0 {
		return errorEnvelope("model_refresh_failed", err.Error()), nil
	}

	models := make([]map[string]interface{}, 0)
	for _, id := range kiloRefresher.Models() {
		models = append(models, modelEntry(id))
	}
	for alias := range modelAliases.Entries() {
		models = append(models, modelEntry(alias))
	}
	result, _ := shared.OKEnvelope(shared.MustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"Models":   models,
	}))
	return result, nil
}

// modelEntry builds one catalog entry for a model ID (upstream or alias).
func modelEntry(id string) map[string]interface{} {
	return map[string]interface{}{
		"ID":                         prefixedModelID(id),
		"Object":                     "model",
		"Created":                    0,
		"OwnedBy":                    PROVIDER_ID,
		"Type":                       PROVIDER_ID,
		"Name":                       id,
		"DisplayName":                id,
		"SupportedGenerationMethods": []string{"chat"},
		"SupportedInputModalities":   []string{"text"},
		"SupportedOutputModalities":  []string{"text"},
		"UserDefined":                false,
	}
}

func handleModelForAuth(rawReq []byte) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return shared.OKEnvelope(`{"Provider":"","AuthID":"","Models":[]}`)
	}

	authID, _ := req["AuthID"].(string)

	models := make([]map[string]interface{}, 0)
	catalogEntries := make([]map[string]interface{}, 0)
	for _, id := range kiloRefresher.Models() {
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

	result, _ := shared.OKEnvelope(shared.MustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"AuthID":   authID,
		"AuthUpdate": map[string]interface{}{
			"model_catalog": base64.StdEncoding.EncodeToString(catalogJSON),
		},
		"Models": models,
	}))
	return result, nil
}
