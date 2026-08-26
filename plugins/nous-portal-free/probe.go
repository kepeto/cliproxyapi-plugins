package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

var nousProbeStores = struct {
	sync.RWMutex
	byScope map[string]storageJSON
}{byScope: make(map[string]storageJSON)}

var nousProber = shared.NewModelProbeScheduler(15*time.Minute, modelHealth, nousProbeTargets, probeNousModel)

func init() {
	nousProber.Start()
}

func rememberNousProbeStore(store storageJSON) {
	if !store.structuralValid() {
		return
	}
	scope := nousHealthScope(store)
	nousProbeStores.Lock()
	nousProbeStores.byScope[scope] = store
	nousProbeStores.Unlock()
}

func nousProbeTargets() []shared.ModelProbeTarget {
	nousProbeStores.RLock()
	stores := make(map[string]storageJSON, len(nousProbeStores.byScope))
	for scope, store := range nousProbeStores.byScope {
		stores[scope] = store
	}
	nousProbeStores.RUnlock()

	targets := make([]shared.ModelProbeTarget, 0)
	seen := make(map[string]struct{})
	for scope, store := range stores {
		if !store.valid() {
			continue
		}
		ids := cachedFreeModelIDs(store)
		if len(ids) == 0 && shared.TrimHTTP(store.InferenceBaseURL) == currentNousInferenceURL() {
			ids = nousRefresher.Models()
		}
		for _, model := range ids {
			key := scope + "\x00" + model
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			targets = append(targets, shared.ModelProbeTarget{Scope: scope, Model: model})
		}
	}
	return targets
}

func probeNousModel(target shared.ModelProbeTarget) shared.ModelProbeOutcome {
	nousProbeStores.RLock()
	store, ok := nousProbeStores.byScope[target.Scope]
	nousProbeStores.RUnlock()
	if !ok || !store.valid() {
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
	url := shared.TrimHTTP(store.InferenceBaseURL) + "/chat/completions"
	body, status, _, err := shared.DoChatRequest(url, store.AccessToken, shared.InjectNousPortalTags(payload))
	if status == 401 || status == 403 {
		return shared.ProbeIgnored
	}
	if err != nil || status != 200 || !shared.ValidChatResponse(body) {
		return shared.ProbeFailed
	}
	return shared.ProbeSucceeded
}
