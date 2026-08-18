package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
)

func handleModelStatic(rawReq []byte) ([]byte, error) {
	_ = rawReq
	models := make([]map[string]interface{}, 0, len(allModels))
	for _, m := range allModels {
		models = append(models, map[string]interface{}{
			"ID": prefixedModelID(m.ID),
			"Object":                     "model",
			"Created":                    0,
			"OwnedBy":                    "opencode-free",
			"Type":                       PROVIDER_ID,
			"DisplayName":                m.Name,
			"Name":                       m.Name,
			"ContextLength":              m.ContextWindow,
			"MaxCompletionTokens":        m.MaxTokens,
			"SupportedGenerationMethods": []string{"chat"},
			"SupportedInputModalities":   []string{"text"},
			"SupportedOutputModalities":  []string{"text"},
			"UserDefined":                false,
		})
	}

		return okEnvelopeJSON(mustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"Models":   models,
	}))
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

	// Health check upstream
	alive := healthCheckOpenCode()

	models := make([]map[string]interface{}, 0)
	catalogEntries := make([]map[string]interface{}, 0)
	for _, m := range allModels {
		if alive {
			models = append(models, map[string]interface{}{
				"ID": prefixedModelID(m.ID),
				"Object":                     "model",
				"Created":                    0,
				"OwnedBy":                    "opencode-free",
				"Type":                       PROVIDER_ID,
				"DisplayName":                m.Name,
				"Name":                       m.Name,
				"ContextLength":              m.ContextWindow,
				"MaxCompletionTokens":        m.MaxTokens,
				"SupportedGenerationMethods": []string{"chat"},
				"SupportedInputModalities":   []string{"text"},
				"SupportedOutputModalities":  []string{"text"},
				"UserDefined":                false,
			})
			catalogEntries = append(catalogEntries, map[string]interface{}{
				"id":   m.ID,
				"name": m.Name,
			})
		}
	}

	catalogJSON, _ := json.Marshal(catalogEntries)

	return okEnvelopeJSON(mustJSON(map[string]interface{}{
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

// healthCheckOpenCode checks if OpenCode /models endpoint is alive
func healthCheckOpenCode() bool {
	req, err := http.NewRequest(http.MethodGet, OPENCODE_MODELS_URL, nil)
	if err != nil {
		return false
	}
	req.Header = http.Header{}
	for k, v := range opencodeHeaders() {
		req.Header.Set(k, v)
	}

	statusCode, _, err := httpDo(req)
	if err != nil {
		return false
	}
	return statusCode == 200
}
