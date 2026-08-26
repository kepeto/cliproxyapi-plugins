package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

const (
	defaultOpenCodeBaseURL   = "https://opencode.ai/zen"
	defaultOpenCodeModelsURL = defaultOpenCodeBaseURL + "/v1/models"
	defaultOpenCodeChatURL   = defaultOpenCodeBaseURL + "/v1/chat/completions"

	PROVIDER_ID = "opencode-free"
	EXECUTOR_ID = "opencode-free"
	PLUGIN_NAME = "OpenCode Free"

	HTTP_TIMEOUT = 30 * time.Second
)

// OpenCode headers (from pi-bansos)
func opencodeHeaders() map[string]string {
	return map[string]string{
		"User-Agent":         "opencode/latest/1.14.50/cli",
		"x-opencode-client":  "cli",
		"x-opencode-project": "default",
		"x-opencode-session": `{"session":"` + randomSessionID() + `"}`,
		"x-opencode-request": randomRequestID(),
		"Accept":             "application/json",
		"Content-Type":       "application/json",
	}
}

func randomSessionID() string {
	return "cli-" + shared.RandomHex(16)
}

func randomRequestID() string {
	return shared.RandomHex(32)
}

var httpClient = &http.Client{Timeout: HTTP_TIMEOUT}

var (
	endpointMu        sync.RWMutex
	opencodeChatURL   = defaultOpenCodeChatURL
	opencodeModelsURL = defaultOpenCodeModelsURL

	// opencodeRefresher periodically fetches the live model catalog from OpenCode.
	opencodeRefresher = shared.NewModelRefresher(
		3*time.Hour,
		fetchOpenCodeModels,
		healthCheckOpenCode,
	)
)

// modelAliases maps client-visible alias IDs to upstream IDs (plugin config).
var modelAliases = shared.NewAliasTable()
var modelHealth = shared.NewModelHealth(3, 15*time.Minute)
var opencodeProber = shared.NewModelProbeScheduler(15*time.Minute, modelHealth, opencodeProbeTargets, probeOpenCodeModel)

func init() {
	opencodeRefresher.Start()
	opencodeProber.Start()
}

// fetchOpenCodeModels retrieves the current free model list from OpenCode.
func fetchOpenCodeModels() ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, currentOpenCodeModelsURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{}
	for k, v := range opencodeHeaders() {
		req.Header.Set(k, v)
	}
	statusCode, body, err := httpDo(req)
	if err != nil {
		return nil, err
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("opencode models returned %d", statusCode)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID == "" {
			continue
		}
		// Only expose free-tier models; paid models require API key.
		lower := strings.ToLower(m.ID)
		if strings.Contains(lower, "-free") || strings.Contains(lower, ":free") {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// healthCheckOpenCode checks if OpenCode /models endpoint is alive
func healthCheckOpenCode() bool {
	req, err := http.NewRequest(http.MethodGet, currentOpenCodeModelsURL(), nil)
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

func opencodeProbeTargets() []shared.ModelProbeTarget {
	scope := openCodeHealthScope()
	ids := opencodeRefresher.Models()
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

func probeOpenCodeModel(target shared.ModelProbeTarget) shared.ModelProbeOutcome {
	if target.Scope != openCodeHealthScope() {
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
	status, body, err := executeOpenCodeChat(payload, false)
	if status == 401 || status == 403 {
		return shared.ProbeIgnored
	}
	if err != nil || status != 200 || !shared.ValidChatResponse(body) {
		return shared.ProbeFailed
	}
	return shared.ProbeSucceeded
}

// config holds plugin-level overrides resolved from plugins.configs.opencode-free.
type config struct {
	BaseURL      string            `json:"opencode_base_url"`
	ChatURL      string            `json:"opencode_chat_url"`
	ModelsURL    string            `json:"opencode_models_url"`
	ModelAliases map[string]string `json:"model_aliases"`
	Prefix       string            `json:"prefix"`
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
	changed := opencodeChatURL != chatURL || opencodeModelsURL != modelsURL
	opencodeChatURL = chatURL
	opencodeModelsURL = modelsURL
	endpointMu.Unlock()
	if changed {
		opencodeRefresher.Reset()
	}
}

func resolveConfig(raw []byte) config {
	cfg := config{}
	// Host forwards the config subtree as YAML bytes; tolerate raw JSON too.
	_ = shared.UnmarshalConfig(raw, &cfg)
	return cfg
}

func (c config) endpoints() (string, string) {
	baseURL := trimHTTP(c.BaseURL)
	if baseURL == "" {
		baseURL = defaultOpenCodeBaseURL
	}
	chatURL := baseURL + "/v1/chat/completions"
	modelsURL := baseURL + "/v1/models"
	if value := trimHTTP(c.ChatURL); value != "" {
		chatURL = value
	}
	if value := trimHTTP(c.ModelsURL); value != "" {
		modelsURL = value
	}
	return chatURL, modelsURL
}

func currentOpenCodeChatURL() string {
	endpointMu.RLock()
	defer endpointMu.RUnlock()
	return opencodeChatURL
}

func currentOpenCodeModelsURL() string {
	endpointMu.RLock()
	defer endpointMu.RUnlock()
	return opencodeModelsURL
}

func openCodeHealthScope() string {
	return PROVIDER_ID + "|" + currentOpenCodeChatURL() + "|" + currentOpenCodeModelsURL()
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
