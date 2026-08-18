package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// modelsDevURL serves provider-agnostic model metadata (capabilities, context
// limits, modalities). Mirrors https://github.com/anomalyco/models.dev.
const modelsDevURL = "https://models.dev/models.json"

// mdModel is the per-model metadata published by models.dev (models.json).
type mdModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Family      string   `json:"family"`
	Knowledge   string   `json:"knowledge"`
	ReleaseDate string   `json:"release_date"`
	LastUpdated string   `json:"last_updated"`
	Reasoning   bool     `json:"reasoning"`
	ToolCall    bool     `json:"tool_call"`
	Structured  bool     `json:"structured_output"`
	Attachment  bool     `json:"attachment"`
	Temperature bool     `json:"temperature"`
	OpenWeights bool     `json:"open_weights"`
	Modalities  struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context"`
		Input   int `json:"input"`
		Output  int `json:"output"`
	} `json:"limit"`
}

var (
	mdMu              sync.RWMutex
	mdCache           map[string]mdModel
	mdLoaded          bool
	mdLoading         bool
	mdRefreshStarted  bool
)

// mdEnsure loads the models.dev catalog on first use. The first caller blocks
// (bounded by the HTTP timeout); later calls are instant. Failures are non-fatal:
// the next call retries, and models simply fall back to their static defaults.
// A background goroutine refreshes the catalog every 24h once the first load lands.
func mdEnsure() {
	mdMu.RLock()
	loaded := mdLoaded
	mdMu.RUnlock()
	if loaded {
		return
	}
	mdMu.Lock()
	if mdLoaded || mdLoading {
		mdMu.Unlock()
		return
	}
	mdLoading = true
	mdMu.Unlock()

	loadModelsDev()

	mdMu.Lock()
	mdLoading = false
	if mdLoaded && !mdRefreshStarted {
		mdRefreshStarted = true
		go func() {
			for {
				time.Sleep(24 * time.Hour)
				loadModelsDev()
			}
		}()
	}
	mdMu.Unlock()
}

// loadModelsDev fetches and stores the models.dev catalog. Safe to call repeatedly.
func loadModelsDev() {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(modelsDevURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var parsed map[string]mdModel
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return
	}
	mdMu.Lock()
	mdCache = parsed
	mdLoaded = true
	mdMu.Unlock()
}

// mdLookup resolves a real (unprefixed) model ID to its models.dev metadata.
// Free upstreams suffix model IDs with ":free"; models.dev keys omit it.
func mdLookup(realID string) (mdModel, bool) {
	mdMu.RLock()
	defer mdMu.RUnlock()
	if mdCache == nil {
		return mdModel{}, false
	}
	if m, ok := mdCache[realID]; ok {
		return m, true
	}
	if stripped := strings.TrimSuffix(realID, ":free"); stripped != realID {
		if m, ok := mdCache[stripped]; ok {
			return m, true
		}
	}
	return mdModel{}, false
}

func modalityString(in, out []string) string {
	join := func(s []string) string {
		if len(s) == 0 {
			return "none"
		}
		return strings.Join(s, "+")
	}
	return join(in) + " -> " + join(out)
}

// enrichModelInfo overlays models.dev capability metadata onto a model info map.
// It keeps the host (CPA) contract fields (ContextLength, SupportedInputModalities,
// ...) populated and additionally exposes OpenAI /v1/models-shaped fields extended
// with OpenRouter-style capability fields (context_length, architecture, reasoning,
// ...). Unknown extra fields are ignored by the host's parser, so adding them is safe.
func enrichModelInfo(info map[string]any, realID string) {
	mdEnsure()
	md, ok := mdLookup(realID)
	if !ok {
		return
	}

	// OpenAI-compatible base fields (mirror the host contract fields).
	info["id"] = info["ID"]
	info["object"] = "model"
	info["owned_by"] = info["OwnedBy"]

	if md.Limit.Context > 0 {
		info["ContextLength"] = md.Limit.Context
		info["context_length"] = md.Limit.Context
		info["InputTokenLimit"] = md.Limit.Input
		info["OutputTokenLimit"] = md.Limit.Output
	}
	if md.Limit.Output > 0 {
		info["MaxCompletionTokens"] = md.Limit.Output
		info["max_completion_tokens"] = md.Limit.Output
	}
	if len(md.Modalities.Input) > 0 {
		info["SupportedInputModalities"] = md.Modalities.Input
		info["input_modalities"] = md.Modalities.Input
	}
	if len(md.Modalities.Output) > 0 {
		info["SupportedOutputModalities"] = md.Modalities.Output
		info["output_modalities"] = md.Modalities.Output
	}
	if md.Description != "" {
		info["Description"] = md.Description
		info["description"] = md.Description
	}
	if md.Name != "" {
		info["DisplayName"] = md.Name
	}
	info["reasoning"] = md.Reasoning
	info["tool_call"] = md.ToolCall
	info["structured_output"] = md.Structured
	info["attachment"] = md.Attachment
	info["temperature"] = md.Temperature
	info["knowledge"] = md.Knowledge
	info["release_date"] = md.ReleaseDate
	info["family"] = md.Family
	info["open_weights"] = md.OpenWeights
	info["architecture"] = map[string]any{
		"input_modalities":  md.Modalities.Input,
		"output_modalities": md.Modalities.Output,
		"modality":          modalityString(md.Modalities.Input, md.Modalities.Output),
	}
	if md.Temperature {
		info["SupportedParameters"] = []string{"temperature"}
	}
	if t, err := time.Parse("2006-01-02", md.ReleaseDate); err == nil {
		info["created"] = t.Unix()
	}
}
