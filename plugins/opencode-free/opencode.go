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
	UPSTREAM_OPENCODE = "https://opencode.ai/zen"
	OPENCODE_API      = UPSTREAM_OPENCODE + "/v1"
	OPENCODE_MODELS_URL = OPENCODE_API + "/models"
	OPENCODE_CHAT_URL   = OPENCODE_API + "/chat/completions"

	PROVIDER_ID  = "opencode-free"
	EXECUTOR_ID  = "opencode-free"
	PLUGIN_NAME  = "OpenCode Free"
	PLUGIN_VERSION = "0.1.0"

	HTTP_TIMEOUT = 30 * time.Second
)

// ModelDef matches pi-bansos model definitions
type ModelDef struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Reasoning     bool   `json:"reasoning"`
	ContextWindow int    `json:"contextWindow"`
	MaxTokens     int    `json:"maxTokens"`
	ThinkingFormat string `json:"thinkingFormat,omitempty"`
}

// OpenCode headers (from pi-bansos)
func opencodeHeaders() map[string]string {
	return map[string]string{
		"User-Agent":           "opencode/latest/1.14.50/cli",
		"x-opencode-client":    "cli",
		"x-opencode-project":   "default",
		"x-opencode-session":   `{"session":"` + randomSessionID() + `"}`,
		"x-opencode-request":   randomRequestID(),
		"Accept":               "application/json",
		"Content-Type":         "application/json",
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func randomSessionID() string {
	return "cli-" + randomHex(16)
}

func randomRequestID() string {
	return randomHex(32)
}

func randomHex(n int) string {
	const chars = "0123456789abcdef"
	b := make([]byte, n)
	httpReadRandom(b)
	for i := range b {
		b[i] = chars[b[i]%16]
	}
	return string(b)
}

func httpReadRandom(b []byte) error {
	// Simple random using time-based seed
	// In production, use crypto/rand
	src := time.Now().UnixNano()
	for i := range b {
		src = (src*6364136223846793005 + 1)
		b[i] = byte(src >> 56)
	}
	return nil
}

var httpClient = &http.Client{Timeout: HTTP_TIMEOUT}

// opencodeRefresher periodically fetches the live model catalog from OpenCode.
var opencodeRefresher = shared.NewModelRefresher(
	3*time.Hour,
	fetchOpenCodeModels,
	healthCheckOpenCode,
)

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

func ioReadAll(r io.Reader) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	for {
		tmp := make([]byte, 4096)
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF {
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
