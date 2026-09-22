package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
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

	HTTP_TIMEOUT = 180 * time.Second
)

// kiloCatalogModel is the live metadata returned by KiloCode's /models API.
type kiloCatalogModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Created      int64  `json:"created"`
	IsFree       bool   `json:"isFree"`
	Context      int    `json:"context_length"`
	Expiration   string `json:"expiration_date"`
	Architecture struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	TopProvider struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
}

var (
	endpointMu    sync.RWMutex
	kiloChatURL   = defaultKiloChatURL
	kiloModelsURL = defaultKiloModelsURL

	kiloCatalogMu sync.RWMutex
	kiloCatalog   = make(map[string]kiloCatalogModel)

	kiloRefresher = shared.NewModelRefresher(
		3*time.Hour,
		fetchKiloCatalog,
		healthCheckKilo,
	)
)

// modelAliases maps client-visible alias IDs to upstream IDs (plugin config).
var modelAliases = shared.NewAliasTable()
var modelHealth = shared.NewModelHealth(3, 15*time.Minute)
var kiloHealthChecksEnabled atomic.Bool
var kiloProber = shared.NewModelProbeScheduler(15*time.Minute, modelHealth, kiloProbeTargets, probeKiloModel)

func init() {
	kiloRefresher.Start()
	kiloProber.Start()
}

func kiloHeaders() map[string]string {
	return map[string]string{
		"Content-Type":          "application/json",
		"Accept":                "application/json",
		"User-Agent":            "opencode-kilo-provider",
		"X-KILOCODE-EDITORNAME": "Kilo CLI",
	}
}

var httpClient = &http.Client{Transport: streamTransport, Timeout: HTTP_TIMEOUT}

// config holds plugin-level overrides resolved from plugins.configs.kilo-free.
type config struct {
	BaseURL      string            `json:"kilo_base_url"`
	ChatURL      string            `json:"kilo_chat_url"`
	ModelsURL    string            `json:"kilo_models_url"`
	Prefix       string            `json:"prefix"`
	ModelAliases map[string]string `json:"model_aliases"`
	HealthCheck  bool              `json:"health_check"`
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
	setKiloHealthChecksEnabled(cfg.HealthCheck)

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

func setKiloHealthChecksEnabled(enabled bool) {
	if kiloHealthChecksEnabled.Swap(enabled) != enabled {
		modelHealth.Reset()
	}
}

func kiloHealthChecksOn() bool {
	return kiloHealthChecksEnabled.Load()
}

func kiloModelHidden(scope, model string) bool {
	return kiloHealthChecksOn() && modelHealth.Hidden(scope, model)
}

func kiloModelAllowed(scope, model string) bool {
	return !kiloHealthChecksOn() || modelHealth.Allow(scope, model)
}

func kiloRecordProbeFailure(scope, model string) {
	if kiloHealthChecksOn() {
		modelHealth.RecordProbeFailure(scope, model)
	}
}

func kiloRecordFailure(scope, model string) {
	if kiloHealthChecksOn() {
		modelHealth.RecordFailure(scope, model)
	}
}

func kiloRecordSuccess(scope, model string) {
	if kiloHealthChecksOn() {
		modelHealth.RecordSuccess(scope, model)
	}
}

func kiloVisibleModels(scope string, models []string) []string {
	if !kiloHealthChecksOn() {
		return models
	}
	return modelHealth.Filter(scope, models)
}

// fetchKiloCatalog retrieves the current free model list and metadata from KiloCode.
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog returned %d", resp.StatusCode)
	}

	var result struct {
		Data []kiloCatalogModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	now := time.Now()
	ids := make([]string, 0, len(result.Data))
	catalog := make(map[string]kiloCatalogModel, len(result.Data))
	for _, model := range result.Data {
		if model.ID == "" || !model.IsFree || kiloCatalogModelExpired(model, now) {
			continue
		}
		if _, duplicate := catalog[model.ID]; duplicate {
			continue
		}
		catalog[model.ID] = model
		ids = append(ids, model.ID)
	}
	kiloCatalogMu.Lock()
	kiloCatalog = catalog
	kiloCatalogMu.Unlock()
	return ids, nil
}

func kiloCatalogModelExpired(model kiloCatalogModel, now time.Time) bool {
	if model.Expiration == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, model.Expiration)
	if err != nil {
		expiresAt, err = time.ParseInLocation("2006-01-02", model.Expiration, time.UTC)
		if err != nil {
			return false
		}
	}
	return !expiresAt.After(now)
}

func kiloCatalogModelFor(id string) (kiloCatalogModel, bool) {
	kiloCatalogMu.RLock()
	defer kiloCatalogMu.RUnlock()
	model, ok := kiloCatalog[id]
	return model, ok
}

func httpDo(req *http.Request) (int, []byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
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
	if !kiloHealthChecksOn() {
		return nil
	}
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
	if !kiloHealthChecksOn() || target.Scope != kiloHealthScope() {
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
