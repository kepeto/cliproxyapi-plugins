package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

// fallbackModels is an audited static catalog of known free-tier IDs. It is
// used only when the live catalog is unavailable or no login exists; paid
// models must never enter this fallback path.
var fallbackModels = []string{
	"minimax/minimax-m2.5:free",
	"tencent/hy3:free",
	"stepfun/step-3.7-flash:free",
	"upstage/solar-pro4:free",
	"meituan/longcat-2.0:free",
}

func modelStaticPayload() string {
	return modelStaticPayloadForScope(nousHealthScope(storageJSON{InferenceBaseURL: currentNousInferenceURL()}))
}

func modelStaticPayloadForScope(scope string) string {
	ids := nousRefresher.Models()
	if len(ids) == 0 {
		ids = fallbackModels
	}
	return modelPayloadForIDs(scope, ids)
}

func fallbackModelPayload(scope string) string {
	return modelPayloadForIDs(scope, fallbackModels)
}

func modelPayloadForIDs(scope string, ids []string) string {
	models := make([]map[string]any, 0, len(ids))
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
		if !modelHealth.Hidden(scope, id) {
			models = append(models, modelInfo(prefixedModelID(id), id))
		}
	}
	for alias, target := range modelAliases.Entries() {
		if _, ok := allowed[target]; !ok || modelHealth.Hidden(scope, target) {
			continue
		}
		models = append(models, modelInfo(prefixedModelID(alias), alias))
	}
	return shared.MustJSON(map[string]any{
		"Provider": ProviderID,
		"Models":   models,
	})
}

func cachedFreeModelIDs(store storageJSON) []string {
	if len(store.ModelCatalog) == 0 {
		return nil
	}
	var catalog []rawCatalogModel
	if json.Unmarshal(store.ModelCatalog, &catalog) != nil {
		return nil
	}
	free := filterFreeModels(catalog)
	ids := make([]string, 0, len(free))
	for _, model := range free {
		if model.ID != "" {
			ids = append(ids, model.ID)
		}
	}
	return ids
}

func cachedModelPayload(store storageJSON, scope string) (string, bool) {
	ids := cachedFreeModelIDs(store)
	if len(ids) == 0 {
		return "", false
	}
	return modelPayloadForIDs(scope, ids), true
}

func freeModelAllowed(store storageJSON, modelID string) bool {
	if modelID == "" {
		return false
	}
	for _, id := range cachedFreeModelIDs(store) {
		if id == modelID {
			return true
		}
	}
	for _, id := range fallbackModels {
		if id == modelID {
			return true
		}
	}
	return strings.HasSuffix(strings.ToLower(modelID), ":free")
}

// handleModelForAuth fetches the live catalog from the inference base URL and
// returns both the model list and an auth update carrying the refreshed catalog.
func handleModelForAuth(raw []byte) ([]byte, error) {
	var req struct {
		StorageJSON []byte `json:"StorageJSON"`
		Provider    string `json:"AuthProvider"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorEnvelope("model_for_auth_failed", "invalid request: "+err.Error()), nil
	}
	store := decodeStorage(req.StorageJSON)
	if !store.valid() {
		return shared.OKEnvelope(modelStaticPayload())
	}

	scope := nousHealthScope(store)
	catalog, err := fetchModelCatalog(store.InferenceBaseURL, store.AccessToken)
	if err != nil {
		if cached, ok := cachedModelPayload(store, scope); ok {
			return shared.OKEnvelope(cached)
		}
		return shared.OKEnvelope(fallbackModelPayload(scope))
	}

	freeModels := filterFreeModels(catalog)
	allowed := make(map[string]struct{}, len(freeModels))
	models := make([]map[string]any, 0, len(freeModels))
	for _, m := range freeModels {
		allowed[m.ID] = struct{}{}
		if !modelHealth.Hidden(scope, m.ID) {
			models = append(models, modelInfo(prefixedModelID(m.ID), m.ID))
		}
	}
	for alias, target := range modelAliases.Entries() {
		if _, ok := allowed[target]; !ok || modelHealth.Hidden(scope, target) {
			continue
		}
		models = append(models, modelInfo(prefixedModelID(alias), alias))
	}
	if len(models) == 0 {
		if cached, ok := cachedModelPayload(store, scope); ok {
			return shared.OKEnvelope(cached)
		}
		return shared.OKEnvelope(fallbackModelPayload(scope))
	}

	// Persist catalog into the auth blob for later reuse. The host merges
	// missing identity fields from the original auth record.
	updated := store
	updated.ModelCatalog, _ = json.Marshal(freeModels)
	storageJSON, _ := json.Marshal(updated)
	auth := map[string]any{"Provider": ProviderID, "StorageJSON": storageJSON}

	return shared.OKEnvelope(shared.MustJSON(map[string]any{
		"Provider":   ProviderID,
		"Models":     models,
		"AuthUpdate": auth,
	}))
}

func filterFreeModels(catalog []rawCatalogModel) []rawCatalogModel {
	freeModels := make([]rawCatalogModel, 0, len(catalog))
	seen := make(map[string]struct{}, len(catalog))
	for _, m := range catalog {
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" {
			continue
		}
		id := strings.ToLower(m.ID)
		name := strings.ToLower(strings.TrimSpace(m.Name))
		if !strings.HasSuffix(id, ":free") && !strings.Contains(name, "free") {
			continue
		}
		if _, ok := seen[m.ID]; ok {
			continue
		}
		seen[m.ID] = struct{}{}
		freeModels = append(freeModels, m)
	}
	return freeModels
}

type rawCatalogModel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IsFree bool   `json:"isFree"`
}

func fetchModelCatalog(baseURL, apiKey string) ([]rawCatalogModel, error) {
	url := baseURL
	if url == "" {
		url = defaultInferenceBaseURL
	}
	url = shared.TrimHTTP(url) + "/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("catalog auth failed: %d", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("catalog request failed: %d", resp.StatusCode)
	}
	var payload struct {
		Data []rawCatalogModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data) == 0 {
		// Some endpoints return a top-level array.
		var arr []rawCatalogModel
		if json.Unmarshal(body, &arr) == nil {
			return arr, nil
		}
	}
	return payload.Data, nil
}

func modelInfo(id, name string) map[string]any {
	return map[string]any{
		"ID":                         id,
		"Object":                     "model",
		"OwnedBy":                    ProviderID,
		"Type":                       ProviderID,
		"DisplayName":                name,
		"Name":                       name,
		"ContextLength":              128000,
		"MaxCompletionTokens":        4096,
		"SupportedGenerationMethods": []string{"chat"},
		"SupportedInputModalities":   []string{"text"},
		"SupportedOutputModalities":  []string{"text"},
		"UserDefined":                false,
	}
}
