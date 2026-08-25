package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	return modelStaticPayloadForScope(nousHealthScope(storageJSON{InferenceBaseURL: defaultInferenceBaseURL}))
}

func modelStaticPayloadForScope(scope string) string {
	models := make([]map[string]any, 0, len(fallbackModels))
	for _, id := range modelHealth.Filter(scope, fallbackModels) {
		models = append(models, modelEntry(id))
	}
	for alias, target := range modelAliases.Entries() {
		if !modelHealth.Hidden(scope, target) {
			models = append(models, modelEntry(alias))
		}
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
		// No usable credential: fall back to static catalog without an auth update.
		return shared.OKEnvelope(modelStaticPayload())
	}

	catalog, err := fetchModelCatalog(store.InferenceBaseURL, store.AccessToken)
	if err != nil {
		// Catalog unavailable: report static list, keep stored catalog if present.
		return shared.OKEnvelope(modelStaticPayload())
	}

	scope := nousHealthScope(store)
	models := make([]map[string]any, 0, len(catalog))
	for _, m := range catalog {
		if !modelHealth.Hidden(scope, m.ID) {
			models = append(models, modelEntry(m.ID))
		}
	}
	for alias, target := range modelAliases.Entries() {
		if !modelHealth.Hidden(scope, target) {
			models = append(models, modelEntry(alias))
		}
	}
	if len(models) == 0 {
		return shared.OKEnvelope(modelStaticPayloadForScope(scope))
	}

	// Persist catalog into the auth blob for later reuse.
	updated := store
	updated.ModelCatalog, _ = json.Marshal(catalog)
	auth := buildAuthData(updated, ProviderID, "nous-portal.json", "Nous Portal", nil)

	return shared.OKEnvelope(shared.MustJSON(map[string]any{
		"Provider":   ProviderID,
		"Models":     models,
		"AuthUpdate": auth,
	}))
}

type rawCatalogModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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

// modelEntry builds one catalog entry for a model ID (upstream or alias).
func modelEntry(id string) map[string]any {
	return map[string]any{
		"ID":                         prefixedModelID(id),
		"Object":                     "model",
		"OwnedBy":                    ProviderID,
		"Type":                       ProviderID,
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
