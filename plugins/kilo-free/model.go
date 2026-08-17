package main

import (
	"encoding/json"
	"net/http"
	"time"
)

func handleModelStatic(rawReq []byte) ([]byte, error) {
	_ = rawReq
	models := make([]map[string]interface{}, 0, len(allModels))
	for _, m := range allModels {
		models = append(models, map[string]interface{}{
			"ID":          m.ID,
			"Object":      "model",
			"Created":     0,
			"OwnedBy":     "kilo-free",
			"Provider":    PROVIDER_ID,
			"Name":        m.Name,
			"Reasoning":   m.Reasoning,
			"ContextWindow": m.ContextWindow,
			"MaxTokens":   m.MaxTokens,
			"Input":       []string{"text"},
			"Cost": map[string]interface{}{
				"Input":  0,
				"Output": 0,
				"CacheRead": 0,
				"CacheWrite": 0,
			},
		})
	}
	return okEnvelopeJSON(mustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"Models":   models,
	}))
}

func handleModelForAuth(rawReq []byte) ([]byte, error) {
	var req map[string]string
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}
	authID := req["AuthID"]
	if authID == "" {
		return errorEnvelope("invalid_request", "missing auth_id"), nil
	}

	// Health check upstream
	alive := healthCheckKilo()

	models := make([]map[string]interface{}, 0)
	for _, m := range allModels {
		if alive {
			models = append(models, map[string]interface{}{
				"ID":          m.ID,
				"Object":      "model",
				"Created":     0,
				"OwnedBy":     "kilo-free",
				"Provider":    PROVIDER_ID,
				"Name":        m.Name,
				"Reasoning":   m.Reasoning,
				"ContextWindow": m.ContextWindow,
				"MaxTokens":   m.MaxTokens,
				"Input":       []string{"text"},
				"Cost": map[string]interface{}{
					"Input":  0,
					"Output": 0,
					"CacheRead": 0,
					"CacheWrite": 0,
				},
			})
		}
	}

	return okEnvelopeJSON(mustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"AuthID":   authID,
		"Models":   models,
		"Upstream": map[string]interface{}{
			"KiloAlive": alive,
			"CheckedAt": time.Now().Format(time.RFC3339),
		},
	}))
}

// healthCheckKilo checks if KiloCode /models endpoint is alive
func healthCheckKilo() bool {
	req, err := http.NewRequest(http.MethodGet, KILO_MODELS_URL, nil)
	if err != nil {
		return false
	}
	req.Header = http.Header{}
	for k, v := range kiloHeaders() {
		req.Header.Set(k, v)
	}

	statusCode, _, err := httpDo(req)
	if err != nil {
		return false
	}
	return statusCode == 200
}
