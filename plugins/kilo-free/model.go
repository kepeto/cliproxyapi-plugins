package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func ensureModels() error {
	if err := kiloRefresher.RefreshIfEmpty(); err != nil && len(kiloRefresher.Models()) == 0 {
		return err
	}
	return nil
}

func handleModelStatic(rawReq []byte) ([]byte, error) {
	if err := keylessAuth.Ensure(rawReq); err != nil {
		return errorEnvelope("auth_bootstrap_failed", err.Error()), nil
	}
	if err := ensureModels(); err != nil {
		return errorEnvelope("model_refresh_failed", err.Error()), nil
	}

	models := make([]map[string]interface{}, 0)
	for _, id := range kiloVisibleModels(kiloHealthScope(), kiloRefresher.Models()) {
		models = append(models, modelEntry(id))
	}
	for alias, target := range modelAliases.Entries() {
		if !kiloModelHidden(kiloHealthScope(), target) {
			models = append(models, modelEntry(alias))
		}
	}
	result, _ := shared.OKEnvelope(shared.MustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"Models":   models,
	}))
	return result, nil
}

// modelEntry builds one catalog entry from live upstream metadata. Aliases
// inherit the metadata of their target through modelEntryFor.
func modelEntry(id string) map[string]interface{} {
	return modelEntryFor(id, id)
}

func modelEntryFor(publicID, lookupID string) map[string]interface{} {
	entry := map[string]interface{}{
		"ID":                         prefixedModelID(publicID),
		"Object":                     "model",
		"Created":                    int64(0),
		"OwnedBy":                    PROVIDER_ID,
		"Type":                       PROVIDER_ID,
		"Name":                       publicID,
		"DisplayName":                publicID,
		"SupportedGenerationMethods": []string{"chat"},
		"UserDefined":                false,
	}
	metadata, ok := kiloCatalogModelFor(lookupID)
	if !ok {
		return entry
	}
	if metadata.Created != 0 {
		entry["Created"] = metadata.Created
	}
	if metadata.Name != "" {
		entry["Name"] = metadata.Name
		entry["DisplayName"] = metadata.Name
	}
	if metadata.Description != "" {
		entry["Description"] = metadata.Description
	}
	if metadata.Context > 0 {
		entry["ContextLength"] = metadata.Context
	}
	if metadata.TopProvider.MaxCompletionTokens > 0 {
		entry["MaxCompletionTokens"] = metadata.TopProvider.MaxCompletionTokens
	}
	if len(metadata.Architecture.InputModalities) > 0 {
		entry["SupportedInputModalities"] = metadata.Architecture.InputModalities
	}
	if len(metadata.Architecture.OutputModalities) > 0 {
		entry["SupportedOutputModalities"] = metadata.Architecture.OutputModalities
	}
	if metadata.Expiration != "" {
		entry["ExpirationDate"] = metadata.Expiration
	}
	return entry
}

func handleModelForAuth(rawReq []byte) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}

	authID, _ := req["AuthID"].(string)
	if err := ensureModels(); err != nil {
		return errorEnvelope("model_refresh_failed", err.Error()), nil
	}

	models := make([]map[string]interface{}, 0)
	catalogEntries := make([]map[string]interface{}, 0)
	for _, id := range kiloVisibleModels(kiloHealthScope(), kiloRefresher.Models()) {
		models = append(models, modelEntry(id))
		catalogEntries = append(catalogEntries, map[string]interface{}{
			"id":   id,
			"name": id,
		})
	}
	for alias, target := range modelAliases.Entries() {
		if !kiloModelHidden(kiloHealthScope(), target) {
			models = append(models, modelEntryFor(alias, target))
		}
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
