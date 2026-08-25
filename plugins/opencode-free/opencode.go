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

const (
	UPSTREAM_OPENCODE   = "https://opencode.ai/zen"
	OPENCODE_API        = UPSTREAM_OPENCODE + "/v1"
	OPENCODE_MODELS_URL = OPENCODE_API + "/models"
	OPENCODE_CHAT_URL   = OPENCODE_API + "/chat/completions"

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

// opencodeRefresher periodically fetches the live model catalog from OpenCode.
var opencodeRefresher = shared.NewModelRefresher(
	3*time.Hour,
	fetchOpenCodeModels,
	healthCheckOpenCode,
)

// modelAliases maps client-visible alias IDs to upstream IDs (plugin config).
var modelAliases = shared.NewAliasTable()

func init() {
	opencodeRefresher.Start()
}

// fetchOpenCodeModels retrieves the current free model list from OpenCode.
func fetchOpenCodeModels() ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, OPENCODE_MODELS_URL, nil)
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

// config holds plugin-level overrides resolved from plugins.configs.opencode-free.
type config struct {
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
}

func resolveConfig(raw []byte) config {
	cfg := config{}
	// Host forwards the config subtree as YAML bytes; tolerate raw JSON too.
	_ = shared.UnmarshalConfig(raw, &cfg)
	return cfg
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
