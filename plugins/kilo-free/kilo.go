package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

const (
	KILO_CHAT_URL   = "https://api.kilo.ai/api/gateway/v1/chat/completions"
	KILO_MODELS_URL = "https://api.kilo.ai/api/gateway/models"

	PROVIDER_ID = "kilo-free"
	EXECUTOR_ID = "kilo-free"
	PLUGIN_NAME = "KiloCode Free"

	HTTP_TIMEOUT = 30 * time.Second
)

// kiloRefresher periodically fetches the live model catalog from KiloCode.
var kiloRefresher = shared.NewModelRefresher(
	3*time.Hour,
	fetchKiloCatalog,
	healthCheckKilo,
)

func init() {
	kiloRefresher.Start()
}

func kiloHeaders() map[string]string {
	return map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

var httpClient = &http.Client{Timeout: HTTP_TIMEOUT}

// config holds plugin-level overrides resolved from plugins.configs.kilo-free.
type config struct {
	Prefix string `json:"prefix"`
}

func (c config) prefix() string {
	if v := trimHTTP(c.Prefix); v != "" {
		return v
	}
	return ""
}

// resolveConfig decodes the plugin config YAML subtree forwarded by the host.
func resolveConfig(raw []byte) config {
	cfg := config{}
	if len(raw) == 0 {
		return cfg
	}
	// The host may send either a raw object or wrap it; be tolerant.
	if err := json.Unmarshal(raw, &cfg); err != nil {
		// Try nested under a generic map.
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) == nil {
			if v, ok := m["config"]; ok {
				_ = json.Unmarshal(v, &cfg)
			}
		}
	}
	return cfg
}

func httpDo(req *http.Request) (int, []byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := ioReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

func ioReadAll(r interface {
	Read([]byte) (int, error)
}) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	for {
		tmp := make([]byte, 4096)
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" || err.Error() == "io.EOF" {
				break
			}
			return buf, err
		}
		if n == 0 {
			break
		}
	}
	return buf, nil
}

// fetchKiloCatalog retrieves the current free model list from KiloCode.
func fetchKiloCatalog() ([]string, error) {
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

	ids := make([]string, 0)
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
			ids = append(ids, id)
		}
	}
	return ids, nil
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
