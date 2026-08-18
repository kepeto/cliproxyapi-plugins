package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var (
	// Async catalog fetch
	kiloCatalogMu sync.RWMutex
	kiloCatalog   = staticKiloModels()
	catalogLoaded bool
)

func handleModelStatic(rawReq []byte) ([]byte, error) {
	_ = rawReq
	
	// Start async catalog fetch if not loaded
	if !catalogLoaded {
		go func() {
			set, err := fetchKiloCatalog()
			if err == nil && len(set) > 0 {
				models := make([]string, 0, len(set))
				for id := range set {
					models = append(models, id)
				}
				kiloCatalogMu.Lock()
				kiloCatalog = models
				catalogLoaded = true
				kiloCatalogMu.Unlock()
			}
		}()
	}
	
	kiloCatalogMu.RLock()
	models := kiloCatalog
	kiloCatalogMu.RUnlock()
	
	responseModels := make([]map[string]interface{}, 0, len(models))
	for _, id := range models {
		responseModels = append(responseModels, map[string]interface{}{
			"ID":                         id,
			"Object":                     "model",
			"Created":                    0,
			"OwnedBy":                    PROVIDER_ID,
			"Type":                       PROVIDER_ID,
			"Name":                       id,
			"DisplayName":                id,
			"SupportedGenerationMethods": []string{"chat"},
			"SupportedInputModalities":   []string{"text"},
			"SupportedOutputModalities":  []string{"text"},
			"UserDefined": false,
		})
	}

	result, _ := okEnvelopeJSON(mustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"Models":   responseModels,
	}))
	return result, nil
}

func handleModelForAuth(rawReq []byte) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return okEnvelopeJSON(`{"Provider":"","AuthID":"","Models":[]}`)
	}

	authID, _ := req["AuthID"].(string)
	
	kiloCatalogMu.RLock()
	models := kiloCatalog
	kiloCatalogMu.RUnlock()
	
	responseModels := make([]map[string]interface{}, 0, len(models))
	catalogEntries := make([]map[string]interface{}, 0, len(models))
	for _, id := range models {
		responseModels = append(responseModels, map[string]interface{}{
			"ID":                         id,
			"Object":                     "model",
			"Created":                    0,
			"OwnedBy":                    PROVIDER_ID,
			"Type":                       PROVIDER_ID,
			"Name":                       id,
			"DisplayName":                id,
			"SupportedGenerationMethods": []string{"chat"},
			"SupportedInputModalities":   []string{"text"},
			"SupportedOutputModalities":  []string{"text"},
			"UserDefined": false,
		})
		catalogEntries = append(catalogEntries, map[string]interface{}{
			"id":   id,
			"name": id,
		})
	}

	catalogJSON, _ := json.Marshal(catalogEntries)

	result, _ := okEnvelopeJSON(mustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"AuthID":   authID,
		"AuthUpdate": map[string]interface{}{
			"model_catalog": base64.StdEncoding.EncodeToString(catalogJSON),
		},
		"Models": responseModels,
	}))
	return result, nil
}

func fetchKiloCatalog() (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, KILO_MODELS_URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer kilo-free")
	req.Header.Set("Accept", "application/json")
	
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("catalog returned %d", resp.StatusCode)
	}
	
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	data, ok := result["data"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("no data in catalog")
	}
	
	set := make(map[string]bool)
	for _, item := range data {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id, ok := m["id"].(string)
		if !ok || id == "" {
			continue
		}
		isFree, _ := m["isFree"].(bool)
		if isFree {
			set[id] = true
		}
	}
	
	return set, nil
}

func staticKiloModels() []string {
	return []string{
		"kilo-auto/free",
		"stepfun/step-3.7-flash:free",
		"openrouter/free",
		"tencent/hy3:free",
		"poolside/laguna-s-2.1:free",
		"poolside/laguna-xs-2.1:free",
		"nvidia/nemotron-3-ultra-550b-a55b:free",
		"nvidia/nemotron-3-super-120b-a12b:free",
		"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free",
		"nvidia/nemotron-3.5-lightning:free",
		"nvidia/nemotron-3.5-content-safety:free",
		"cohere/north-mini-code:free",
		"z-ai/glm-5.2:free",
		"liquid/lfm-2.5-2.6b:free",
		"dots-studio/dots-3-note-preview:free",
	}
}

// healthCheckKilo checks if KiloCode /models endpoint is alive
func healthCheckKilo() bool {
	req, err := http.NewRequest(http.MethodGet, KILO_MODELS_URL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer kilo-free")
	req.Header.Set("Accept", "application/json")

	statusCode, _, err := httpDo(req)
	if err != nil {
		return false
	}
	return statusCode == 200
}
