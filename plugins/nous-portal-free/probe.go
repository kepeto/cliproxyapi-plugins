package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

var nousProbeStores = struct {
	sync.RWMutex
	byScope      map[string]storageJSON
	refreshAfter map[string]time.Time
}{byScope: make(map[string]storageJSON), refreshAfter: make(map[string]time.Time)}

var nousProber = shared.NewModelProbeScheduler(15*time.Minute, modelHealth, nousProbeTargets, probeNousModel)

// InternalRefreshInterval is deliberately coarse: the plugin makes no network
// request unless an account is inside the existing two-minute refresh lead.
const internalRefreshInterval = time.Minute

func init() {
	nousProber.Start()
	go internalRefreshLoop()
}

func internalRefreshLoop() {
	ticker := time.NewTicker(internalRefreshInterval)
	defer ticker.Stop()
	for range ticker.C {
		nousProbeStores.RLock()
		stores := make([]storageJSON, 0, len(nousProbeStores.byScope))
		for _, store := range nousProbeStores.byScope {
			stores = append(stores, store)
		}
		nousProbeStores.RUnlock()
		for _, store := range stores {
			if _, ok := refreshNousStoreIfNeeded(store); ok {
				continue
			}
		}
	}
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

// effectiveNousStore overlays the latest internally refreshed credential over
// the host request snapshot. CPA may retain the old StorageJSON after the host
// scheduler misses a plugin refresh, so this cache keeps inference live.
func effectiveNousStore(store storageJSON) storageJSON {
	scope := nousHealthScope(store)
	nousProbeStores.RLock()
	cached, ok := nousProbeStores.byScope[scope]
	nousProbeStores.RUnlock()
	if ok && cached.AccessToken != "" && cached.ExpiresAt.After(store.ExpiresAt) {
		return cached
	}
	return store
}

func refreshNousStoreIfNeeded(store storageJSON) (storageJSON, bool) {
	store = effectiveNousStore(store)
	if store.accessTokenUsable() {
		return store, true
	}
	scope := nousHealthScope(store)
	now := time.Now()
	nousProbeStores.Lock()
	if retryAt := nousProbeStores.refreshAfter[scope]; now.Before(retryAt) {
		nousProbeStores.Unlock()
		return store, false
	}
	nousProbeStores.refreshAfter[scope] = now.Add(5 * time.Minute)
	nousProbeStores.Unlock()
	next, err := refreshStoredAuth(store)
	if err != nil {
		return store, false
	}
	// Keep runtime availability even if an older CPA host lacks host.auth.save;
	// the next refresh cycle retries persistence through the same host bridge.
	_ = persistNousAuth(next)
	rememberNousProbeStore(next)
	nousProbeStores.Lock()
	delete(nousProbeStores.refreshAfter, scope)
	nousProbeStores.Unlock()
	return next, true
}

func nousProbeTargets() []shared.ModelProbeTarget {
	if !nousHealthChecksOn() {
		return nil
	}
	nousProbeStores.RLock()
	stores := make(map[string]storageJSON, len(nousProbeStores.byScope))
	for scope, store := range nousProbeStores.byScope {
		stores[scope] = store
	}
	nousProbeStores.RUnlock()

	targets := make([]shared.ModelProbeTarget, 0)
	seen := make(map[string]struct{})
	for scope, store := range stores {
		if !store.accessTokenUsable() || !store.ModelCatalogAt.IsZero() && time.Since(store.ModelCatalogAt) > modelCatalogMaxAge {
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
	if !nousHealthChecksOn() {
		return shared.ProbeIgnored
	}
	nousProbeStores.RLock()
	store, ok := nousProbeStores.byScope[target.Scope]
	nousProbeStores.RUnlock()
	if !ok || !store.accessTokenUsable() {
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
