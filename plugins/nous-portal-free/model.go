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

// fallbackModels mirrors the Nous Portal freeRecommendedModels list shown by
// Hermes. It is used only when the live catalog is unavailable or no login
// exists; paid models must never enter this fallback path.
var fallbackModels = []string{
	"upstage/solar-pro4:free",
	"meituan/longcat-2.0:free",
	"poolside/laguna-s-2.1:free",
	"poolside/laguna-xs-2.1:free",
	"inclusionai/ling-3.0-flash-fin:free",
	"inclusionai/ling-3.0-flash-sante:free",
	"stepfun/step-3.7-flash:free",
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
	if !store.accessTokenUsable() {
		return shared.OKEnvelope(modelStaticPayload())
	}
	rememberNousProbeStore(store)

	scope := nousHealthScope(store)
	// Portal recommended free models need no auth and match the Hermes free
	// list. Union them with the authenticated inference catalog so account
	// entitlements can only add models, never remove Portal free models.
	portalFree := fetchPortalFreeModels(firstNonEmpty(store.PortalBaseURL, currentNousPortalURL()))
	catalog, err := fetchModelCatalog(store.InferenceBaseURL, store.AccessToken)
	if err != nil && len(portalFree) == 0 {
		if cached, ok := cachedModelPayload(store, scope); ok {
			return shared.OKEnvelope(cached)
		}
		return shared.OKEnvelope(fallbackModelPayload(scope))
	}
	var inferenceFree []rawCatalogModel
	if err == nil {
		inferenceFree = filterFreeModels(catalog)
	}

	freeModels := unionFreeModels(portalFree, inferenceFree)
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
	rememberNousProbeStore(updated)
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

// portalRecommendedModel is one entry of the Portal recommended-models
// endpoint. Only modelName is needed; tokenPrice/source are informational.
type portalRecommendedModel struct {
	ModelName string `json:"modelName"`
	Source    string `json:"source"`
}

// fetchPortalFreeModels returns the Portal freeRecommendedModels list, the
// same source Hermes uses for its free catalog. The endpoint is public and
// needs no authentication.
func fetchPortalFreeModels(portalBaseURL string) []rawCatalogModel {
	url := portalBaseURL
	if url == "" {
		url = defaultPortalBaseURL
	}
	url = shared.TrimHTTP(url) + "/api/nous/recommended-models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	var payload struct {
		Free []portalRecommendedModel `json:"freeRecommendedModels"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	free := make([]rawCatalogModel, 0, len(payload.Free))
	seen := make(map[string]struct{}, len(payload.Free))
	for _, m := range payload.Free {
		id := strings.TrimSpace(m.ModelName)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		free = append(free, rawCatalogModel{ID: id, Name: id})
	}
	return free
}

// unionFreeModels merges portal recommended free models with the
// authenticated inference catalog, deduplicated by model ID.
func unionFreeModels(portal, inference []rawCatalogModel) []rawCatalogModel {
	merged := make([]rawCatalogModel, 0, len(portal)+len(inference))
	seen := make(map[string]struct{}, len(portal)+len(inference))
	for _, list := range [][]rawCatalogModel{portal, inference} {
		for _, m := range list {
			if m.ID == "" {
				continue
			}
			if _, ok := seen[m.ID]; ok {
				continue
			}
			seen[m.ID] = struct{}{}
			merged = append(merged, m)
		}
	}
	return merged
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
