package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

const (
	defaultKiloBaseURL   = "https://api.kilo.ai/api/gateway"
	defaultKiloChatURL   = defaultKiloBaseURL + "/v1/chat/completions"
	defaultKiloModelsURL = defaultKiloBaseURL + "/models"

	PROVIDER_ID = "kilo-free"
	EXECUTOR_ID = "kilo-free"
	PLUGIN_NAME = "KiloCode Free"

	HTTP_TIMEOUT = 30 * time.Second
)

// kiloRefresher periodically fetches the live model catalog from KiloCode.
var (
	endpointMu    sync.RWMutex
	kiloChatURL   = defaultKiloChatURL
	kiloModelsURL = defaultKiloModelsURL

	kiloRefresher = shared.NewModelRefresher(
		3*time.Hour,
		fetchKiloCatalog,
		healthCheckKilo,
	)
)

// modelAliases maps client-visible alias IDs to upstream IDs (plugin config).
var modelAliases = shared.NewAliasTable()
var modelHealth = shared.NewModelHealth(3, 15*time.Minute)
var kiloProber = shared.NewModelProbeScheduler(15*time.Minute, modelHealth, kiloProbeTargets, probeKiloModel)

func init() {
	kiloRefresher.Start()
	kiloProber.Start()
}

func kiloHeaders() map[string]string {
	return map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}
}

var httpClient = &http.Client{Timeout: HTTP_TIMEOUT}

// config holds plugin-level overrides resolved from plugins.configs.kilo-free.
type config struct {
	BaseURL      string            `json:"kilo_base_url"`
	ChatURL      string            `json:"kilo_chat_url"`
	ModelsURL    string            `json:"kilo_models_url"`
	Prefix       string            `json:"prefix"`
	ModelAliases map[string]string `json:"model_aliases"`
}

func (c config) prefix() string {
	if v := trimHTTP(c.Prefix); v != "" {
		return v
	}
	return ""
}

// resolveConfig decodes the plugin config YAML subtree forwarded by the host.
// applyHostAliases merges dashboard-managed oauth-model-alias entries relayed by
// the host inside auth.* request payloads. No-op when none are present.
func applyHostAliases(raw []byte) {
	if host, ok := shared.HostModelAliases(raw, PROVIDER_ID); ok {
		modelAliases.SetHost(host)
	}
}

// applyConfig applies the host-forwarded config subtree (prefix, aliases).
func applyConfig(raw []byte) {
	cfg := resolveConfig(shared.ConfigBytesFromLifecycle(raw))
	setPluginPrefix(cfg.prefix())
	modelAliases.SetConfig(cfg.ModelAliases)

	chatURL, modelsURL := cfg.endpoints()
	endpointMu.Lock()
	changed := kiloChatURL != chatURL || kiloModelsURL != modelsURL
	kiloChatURL = chatURL
	kiloModelsURL = modelsURL
	endpointMu.Unlock()
	if changed {
		kiloRefresher.Reset()
	}
}

func resolveConfig(raw []byte) config {
	cfg := config{}
	// Host forwards the config subtree as YAML bytes; tolerate raw JSON too.
	_ = shared.UnmarshalConfig(raw, &cfg)
	return cfg
}

func (c config) endpoints() (string, string) {
	chatURL := defaultKiloChatURL
	modelsURL := defaultKiloModelsURL
	if baseURL := trimHTTP(c.BaseURL); baseURL != "" {
		chatURL = baseURL + "/v1/chat/completions"
		modelsURL = baseURL + "/models"
	}
	if value := trimHTTP(c.ChatURL); value != "" {
		chatURL = value
	}
	if value := trimHTTP(c.ModelsURL); value != "" {
		modelsURL = value
	}
	return chatURL, modelsURL
}

func currentKiloChatURL() string {
	endpointMu.RLock()
	defer endpointMu.RUnlock()
	return kiloChatURL
}

func currentKiloModelsURL() string {
	endpointMu.RLock()
	defer endpointMu.RUnlock()
	return kiloModelsURL
}

func kiloHealthScope() string {
	return PROVIDER_ID + "|" + currentKiloChatURL() + "|" + currentKiloModelsURL()
}

func httpDo(req *http.Request) (int, []byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

// fetchKiloCatalog retrieves the current free model list from KiloCode.
func fetchKiloCatalog() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentKiloModelsURL(), nil)
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
	req, err := http.NewRequest(http.MethodGet, currentKiloModelsURL(), nil)
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

func kiloProbeTargets() []shared.ModelProbeTarget {
	scope := kiloHealthScope()
	ids := kiloRefresher.Models()
	targets := make([]shared.ModelProbeTarget, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		targets = append(targets, shared.ModelProbeTarget{Scope: scope, Model: id})
	}
	return targets
}

func probeKiloModel(target shared.ModelProbeTarget) shared.ModelProbeOutcome {
	if target.Scope != kiloHealthScope() {
		return shared.ProbeIgnored
	}
	payload, err := json.Marshal(map[string]any{
		"model":      target.Model,
		"messages":   []map[string]string{{"role": "user", "content": "Reply with OK."}},
		"max_tokens": 16,
		"stream":     false,
	})
	if err != nil {
		return shared.ProbeFailed
	}
	status, body, err := executeKiloChat(payload, false)
	if status == 401 || status == 403 {
		return shared.ProbeIgnored
	}
	if err != nil || status != 200 || !shared.ValidChatResponse(body) {
		return shared.ProbeFailed
	}
	return shared.ProbeSucceeded
}
