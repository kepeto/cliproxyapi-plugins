package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func handleModelStatic(rawReq []byte) ([]byte, error) {
	_ = rawReq

	models := make([]map[string]interface{}, 0)
	for _, id := range kiloRefresher.Models() {
		models = append(models, map[string]interface{}{
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
		})
	}

	result, _ := shared.OKEnvelope(shared.MustJSON(map[string]interface{}{
		"Provider": PROVIDER_ID,
		"Models":   models,
	}))
	return result, nil
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
		models = append(models, map[string]interface{}{
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
		})
		catalogEntries = append(catalogEntries, map[string]interface{}{
			"id":   id,
			"name": id,
		})
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
