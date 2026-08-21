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

// fallbackModels is a static, always-available catalog. Mirrors the upstream
// provider's FALLBACK_MODEL_IDS so /v1/models works before any login completes.
var fallbackModels = []string{
	"moonshotai/kimi-k2.6",
	"xiaomi/mimo-v2.5-pro",
	"xiaomi/mimo-v2.5",
	"tencent/hy3-preview",
	"anthropic/claude-opus-4.7",
	"anthropic/claude-opus-4.6",
	"anthropic/claude-sonnet-4.6",
	"anthropic/claude-sonnet-4.5",
	"anthropic/claude-haiku-4.5",
	"openai/gpt-5.5",
	"openai/gpt-5.4-mini",
	"openai/gpt-5.3-codex",
	"google/gemini-3-pro-preview",
	"google/gemini-3-flash-preview",
	"google/gemini-3.1-pro-preview",
	"google/gemini-3.1-flash-lite-preview",
	"qwen/qwen3.5-plus-02-15",
	"qwen/qwen3.5-35b-a3b",
	"stepfun/step-3.5-flash",
	"minimax/minimax-m2.7",
	"minimax/minimax-m2.5",
	"minimax/minimax-m2.5:free",
	"z-ai/glm-5.1",
	"z-ai/glm-5v-turbo",
	"z-ai/glm-5-turbo",
	"x-ai/grok-4.20-beta",
	"nvidia/nemotron-3-super-120b-a12b",
	"arcee-ai/trinity-large-thinking",
	"openai/gpt-5.5-pro",
	"openai/gpt-5.4-nano",
}

func modelStaticPayload() string {
	models := make([]map[string]any, 0)
	ids := nousRefresher.Models()
	if len(ids) == 0 {
		ids = fallbackModels
	}
	for _, id := range ids {
		models = append(models, modelInfo(prefixedModelID(id), id))
	}
	for alias := range modelAliases.Entries() {
		models = append(models, modelInfo(prefixedModelID(alias), alias))
	}
	return shared.MustJSON(map[string]any{
		"Provider": ProviderID,
		"Models":   models,
	})
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

	// Try to refresh from upstream if catalog is stale or missing.
	catalog, err := fetchModelCatalog(store.InferenceBaseURL, store.AccessToken)
	if err != nil {
		// Fallback to cached or static list.
		models := make([]map[string]any, 0)
		ids := nousRefresher.Models()
		if len(ids) == 0 {
			ids = fallbackModels
		}
		for _, id := range ids {
			models = append(models, modelInfo(prefixedModelID(id), id))
		}
		return shared.OKEnvelope(shared.MustJSON(map[string]any{
			"Provider": ProviderID,
			"Models":   models,
		}))
	}

	freeModels := filterFreeModels(catalog)
	models := make([]map[string]any, 0, len(freeModels))
	for _, m := range freeModels {
		models = append(models, modelInfo(prefixedModelID(m.ID), m.ID))
	}
	for alias := range modelAliases.Entries() {
		models = append(models, modelInfo(prefixedModelID(alias), alias))
	}
	if len(models) == 0 {
		return shared.OKEnvelope(modelStaticPayload())
	}

	// Persist catalog into the auth blob for later reuse.
	updated := store
	updated.ModelCatalog, _ = json.Marshal(freeModels)
	auth := buildAuthData(updated, ProviderID, "nous-portal-free.json", "Nous Portal Free", nil)

	return shared.OKEnvelope(shared.MustJSON(map[string]any{
		"Provider":   ProviderID,
		"Models":     models,
		"AuthUpdate": auth,
	}))
}

func filterFreeModels(catalog []rawCatalogModel) []rawCatalogModel {
	freeModels := make([]rawCatalogModel, 0)
	for _, m := range catalog {
		id := strings.ToLower(m.ID)
		name := strings.ToLower(m.Name)
		if strings.Contains(id, "free") || strings.Contains(name, "free") {
			freeModels = append(freeModels, m)
		}
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
