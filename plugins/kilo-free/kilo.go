package main

import (
	"encoding/json"
	"net/http"
	"time"
)

const (
	KILO_CHAT_URL    = "https://api.kilo.ai/api/gateway/chat/completions"
	KILO_MODELS_URL  = "https://api.kilo.ai/api/gateway/models"
	KILO_API_BASE    = "https://api.kilo.ai/api/gateway"

	PROVIDER_ID      = "kilo-free"
	EXECUTOR_ID      = "kilo-free"
	PLUGIN_NAME      = "KiloCode Free"
	PLUGIN_VERSION   = "0.1.0"
	KILO_API_KEY     = "kilo-free"

	HTTP_TIMEOUT     = 30 * time.Second
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

// Known KiloCode free models (from pi-bansos)
var KNOWN_MODELS = []ModelDef{
	{ID: "kilo-auto/free", Name: "Kilo Auto Free", Reasoning: false, ContextWindow: 256_000, MaxTokens: 10_000},
	{ID: "stepfun/step-3.7-flash:free", Name: "Step 3.7 Flash Free", Reasoning: false, ContextWindow: 262_144, MaxTokens: 262_144},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b:free", Name: "Nemotron 3 Ultra Free", Reasoning: true, ContextWindow: 1_000_000, MaxTokens: 65_536, ThinkingFormat: "openrouter"},
	{ID: "nvidia/nemotron-3-super-120b-a12b:free", Name: "Nemotron 3 Super Free", Reasoning: true, ContextWindow: 262_144, MaxTokens: 262_144, ThinkingFormat: "openrouter"},
	{ID: "cohere/north-mini-code:free", Name: "North Mini Code Free", Reasoning: false, ContextWindow: 256_000, MaxTokens: 64_000},
	{ID: "poolside/laguna-xs-2.1:free", Name: "Laguna XS 2.1 Free", Reasoning: false, ContextWindow: 262_144, MaxTokens: 32_768},
	{ID: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free", Name: "Nemotron 3 Nano Omni Free", Reasoning: true, ContextWindow: 256_000, MaxTokens: 65_536, ThinkingFormat: "openrouter"},
	{ID: "openrouter/free", Name: "OpenRouter Free (auto)", Reasoning: false, ContextWindow: 200_000, MaxTokens: 65_536},
	{ID: "nvidia/nemotron-3.5-lightning:free", Name: "Nemotron 3.5 Lightning Free", Reasoning: true, ContextWindow: 1_000_000, MaxTokens: 65_536, ThinkingFormat: "openrouter"},
	{ID: "nvidia/nemotron-3.5-content-safety:free", Name: "Nemotron 3.5 Content Safety Free", Reasoning: true, ContextWindow: 128_000, MaxTokens: 8_192, ThinkingFormat: "openrouter"},
	{ID: "tencent/hy3:free", Name: "Tencent Hy3 Free", Reasoning: true, ContextWindow: 262_144, MaxTokens: 128_000, ThinkingFormat: "openrouter"},
	{ID: "liquid/lfm-2.5-2.6b:free", Name: "Liquid LFM 2.5 2.6B Free", Reasoning: false, ContextWindow: 128_000, MaxTokens: 8_192},
	{ID: "poolside/laguna-s-2.1:free", Name: "Laguna S 2.1 Free", Reasoning: true, ContextWindow: 262_144, MaxTokens: 32_768, ThinkingFormat: "openrouter"},
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

func kiloHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + KILO_API_KEY,
		"Content-Type":  "application/json",
		"Accept":        "application/json",
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func randomHex(n int) string {
	const chars = "0123456789abcdef"
	b := make([]byte, n)
	src := time.Now().UnixNano()
	for i := range b {
		src = (src*6364136223846793005 + 1)
		b[i] = chars[int(src>>56)%16]
	}
	return string(b)
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
