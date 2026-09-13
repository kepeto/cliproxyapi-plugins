package main

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func ensureModels() error {
	if err := opencodeRefresher.RefreshIfEmpty(); err != nil && len(opencodeRefresher.Models()) == 0 {
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
	for _, id := range modelHealth.Filter(openCodeHealthScope(), opencodeRefresher.Models()) {
		models = append(models, modelEntry(id, id))
	}
	for alias, target := range modelAliases.Entries() {
		if !modelHealth.Hidden(openCodeHealthScope(), target) {
			models = append(models, modelEntry(alias, target))
		}
	}

	return shared.OKEnvelope(shared.MustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"Models":   models,
	}))
}

// modelEntry builds one catalog entry from the joined live catalog + official
// metadata. publicID is what the host sees; lookupID is the upstream model used
// for metadata lookup, so aliases inherit the metadata of their target.
func modelEntry(publicID, lookupID string) map[string]interface{} {
	entry := map[string]interface{}{
		"ID":          prefixedModelID(publicID),
		"Object":      "model",
		"Created":     int64(0),
		"OwnedBy":     PROVIDER_ID,
		"Type":        PROVIDER_ID,
		"DisplayName": publicID,
		"Name":        publicID,
		"UserDefined": false,
	}
	model, found := openCodeCatalogModelFor(lookupID)
	if !found {
		return entry
	}
	metadata := model.Metadata
	entry["SupportedGenerationMethods"] = []string{"chat"}
	entry["Created"] = model.Created
	if model.OwnedBy != "" {
		entry["OwnedBy"] = model.OwnedBy
	}
	if metadata.Name != "" {
		entry["DisplayName"] = metadata.Name
		entry["Name"] = metadata.Name
	}
	if metadata.Description != "" {
		entry["Description"] = metadata.Description
	}
	if metadata.Limit.Context > 0 {
		entry["ContextLength"] = metadata.Limit.Context
	}
	if metadata.Limit.Output > 0 {
		entry["MaxCompletionTokens"] = metadata.Limit.Output
	}
	if len(metadata.Modalities.Input) > 0 {
		entry["SupportedInputModalities"] = metadata.Modalities.Input
	}
	if len(metadata.Modalities.Output) > 0 {
		entry["SupportedOutputModalities"] = metadata.Modalities.Output
	}
	// Capability flags come straight from metadata; nothing is inferred.
	entry["Reasoning"] = metadata.Reasoning
	entry["ToolCall"] = metadata.ToolCall
	entry["Attachment"] = metadata.Attachment
	entry["StructuredOutput"] = metadata.StructuredOutput
	entry["Temperature"] = metadata.Temperature
	return entry
}

// catalogEntry builds the dashboard catalog row (id + display name) for the
// auth update, using the official metadata name when available.
func catalogEntry(publicID, lookupID string) map[string]interface{} {
	name := publicID
	if model, found := openCodeCatalogModelFor(lookupID); found && model.Metadata.Name != "" {
		name = model.Metadata.Name
	}
	return map[string]interface{}{
		"id":   publicID,
		"name": name,
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
	if err := ensureModels(); err != nil {
		return errorEnvelope("model_refresh_failed", err.Error()), nil
	}

	alive := opencodeRefresher.Healthy()

	models := make([]map[string]interface{}, 0)
	catalogEntries := make([]map[string]interface{}, 0)
	for _, id := range modelHealth.Filter(openCodeHealthScope(), opencodeRefresher.Models()) {
		models = append(models, modelEntry(id, id))
		catalogEntries = append(catalogEntries, catalogEntry(id, id))
	}
	for alias, target := range modelAliases.Entries() {
		if !modelHealth.Hidden(openCodeHealthScope(), target) {
			models = append(models, modelEntry(alias, target))
		}
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
