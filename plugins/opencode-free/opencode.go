package main

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
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

// Known OpenCode free models (from pi-bansos)
var KNOWN_MODELS = []ModelDef{
	{ID: "deepseek-v4-flash-free", Name: "DeepSeek V4 Flash", Reasoning: true, ContextWindow: 1_000_000, MaxTokens: 384_000},
	{ID: "mimo-v2.5-free", Name: "Mimo V2.5 Free", Reasoning: false, ContextWindow: 1_048_576, MaxTokens: 131_072},
	{ID: "nemotron-3-ultra-free", Name: "Nemotron 3 Ultra", Reasoning: true, ContextWindow: 1_000_000, MaxTokens: 65_536},
	{ID: "north-mini-code-free", Name: "North Mini Code", Reasoning: true, ContextWindow: 256_000, MaxTokens: 64_000},
	{ID: "big-pickle", Name: "Big Pickle", Reasoning: true, ContextWindow: 200_000, MaxTokens: 32_000},
	{ID: "ling-3.0-flash-free", Name: "Ling 3.0 Flash", Reasoning: true, ContextWindow: 262_144, MaxTokens: 32_768},
	{ID: "laguna-s-2.1-free", Name: "Laguna S 2.1 Free", Reasoning: true, ContextWindow: 262_144, MaxTokens: 32_768},
}

var (
	allModels []ModelDef
	modelIDs  = make(map[string]bool)
)

func init() {
	allModels = KNOWN_MODELS
	for _, m := range allModels {
		modelIDs[m.ID] = true
	}
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
