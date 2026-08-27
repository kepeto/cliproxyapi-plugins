package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func TestRegisterPayload(t *testing.T) {
	payload := registerPayload()
	if len(payload) == 0 {
		t.Fatal("registerPayload() returned empty string")
	}

	var meta map[string]any
	if err := json.Unmarshal([]byte(payload), &meta); err != nil {
		t.Fatalf("registerPayload() returned invalid JSON: %v", err)
	}

	metadata, ok := meta["metadata"].(map[string]any)
	if !ok {
		t.Fatal("registerPayload() missing metadata")
	}

	name, ok := metadata["Name"].(string)
	if !ok || name == "" {
		t.Error("registerPayload() metadata missing Name")
	}

	prefix, ok := metadata["Prefix"].(string)
	if !ok || prefix == "" {
		t.Error("registerPayload() metadata missing Prefix")
	}

	version, ok := metadata["Version"].(string)
	if !ok || version == "" {
		t.Error("registerPayload() metadata missing Version")
	}

	configFields, ok := metadata["ConfigFields"].([]any)
	if !ok {
		t.Error("registerPayload() metadata missing ConfigFields")
	}

	// Check that prefix config field exists
	foundPrefix := false
	for _, cf := range configFields {
		field, ok := cf.(map[string]any)
		if !ok {
			continue
		}
		if field["Name"] == "prefix" {
			foundPrefix = true
			break
		}
	}
	if !foundPrefix {
		t.Error("registerPayload() ConfigFields missing prefix field")
	}
}

func TestPluginPrefixDefault(t *testing.T) {
	expected := "nous-portal-free"
	if currentPrefix() != expected {
		t.Errorf("currentPrefix() = %q, want %q", currentPrefix(), expected)
	}
}

func TestPluginPrefixOverride(t *testing.T) {
	orig := currentPrefix()
	setPluginPrefix("custom-prefix")
	if currentPrefix() != "custom-prefix" {
		t.Errorf("currentPrefix() = %q, want %q", currentPrefix(), "custom-prefix")
	}
	setPluginPrefix(orig)
}

func TestPrefixedModelID(t *testing.T) {
	got := prefixedModelID("tencent/hy3:free")
	if got != "nous-portal-free/tencent/hy3:free" {
		t.Errorf("prefixedModelID() = %q, want %q", got, "nous-portal-free/tencent/hy3:free")
	}
}

func TestStripModelPrefix(t *testing.T) {
	got := stripModelPrefix("nous-portal-free/tencent/hy3:free")
	if got != "tencent/hy3:free" {
		t.Errorf("stripModelPrefix() = %q, want %q", got, "tencent/hy3:free")
	}
}

func TestFreeFallbackCatalogAndAliasSafety(t *testing.T) {
	originalRefresher := nousRefresher
	defer func() {
		nousRefresher = originalRefresher
		modelAliases.SetConfig(nil)
		modelAliases.SetHost(nil)
	}()
	nousRefresher = shared.NewModelRefresher(time.Hour, nil, nil)
	modelAliases.SetConfig(map[string]string{
		"paid-alias": "openai/gpt-5.5",
		"free-alias": fallbackModels[0],
	})

	var payload struct {
		Provider string `json:"Provider"`
		Models   []struct {
			ID string `json:"ID"`
		} `json:"Models"`
	}
	if err := json.Unmarshal([]byte(modelStaticPayload()), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Provider != ProviderID {
		t.Fatalf("Provider = %q, want %q", payload.Provider, ProviderID)
	}
	if len(payload.Models) != len(fallbackModels)+1 {
		t.Fatalf("Models = %d, want %d", len(payload.Models), len(fallbackModels)+1)
	}
	seenAlias := false
	for _, model := range payload.Models {
		id := stripModelPrefix(model.ID)
		if id == "paid-alias" {
			t.Fatal("paid alias leaked into free catalog")
		}
		if id == "free-alias" {
			seenAlias = true
			continue
		}
		found := false
		for _, fallback := range fallbackModels {
			if id == fallback {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("unexpected fallback model %q", id)
		}
	}
	if !seenAlias {
		t.Fatal("free alias missing from catalog")
	}
}

func TestFilterFreeModelsStrictAndDeduplicates(t *testing.T) {
	got := filterFreeModels([]rawCatalogModel{
		{ID: "provider/freebird"},
		{ID: "provider/model:free"},
		{ID: "provider/model:free"},
		{ID: "provider/paid", Name: "Free tier"},
		{ID: "provider/paid-name", Name: "Paid model"},
		{ID: "", Name: "Free model"},
	})
	if len(got) != 2 {
		t.Fatalf("filtered models = %#v, want two documented free matches", got)
	}
	if got[0].ID != "provider/model:free" || got[1].ID != "provider/paid" {
		t.Fatalf("unexpected filtered models: %#v", got)
	}
}

func TestModelForAuthUsesFilteredCacheOnFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	cache, err := json.Marshal([]rawCatalogModel{
		{ID: "cached/model:free"},
		{ID: "paid/model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := storageJSON{
		AccessToken:      "token",
		InferenceBaseURL: server.URL,
		AccountID:        "account-a",
		ModelCatalog:     cache,
	}
	storage, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{"StorageJSON": storage})
	if err != nil {
		t.Fatal(err)
	}

	response, _ := handleModelForAuth(request)
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			Models     []map[string]any `json:"Models"`
			AuthUpdate json.RawMessage  `json:"AuthUpdate"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || len(envelope.Result.Models) != 1 {
		t.Fatalf("unexpected cached fallback response: %s", response)
	}
	if len(envelope.Result.AuthUpdate) != 0 {
		t.Fatal("fetch failure emitted destructive AuthUpdate")
	}
	if envelope.Result.Models[0]["ID"] != prefixedModelID("cached/model:free") {
		t.Fatalf("unexpected cached model: %#v", envelope.Result.Models[0])
	}
}

func TestNousFreeReconfigureRetargetsRefresher(t *testing.T) {
	originalURL := currentNousInferenceURL()
	originalRefresher := nousRefresher
	defer func() {
		nousRefresher = originalRefresher
		setNousInferenceURL(originalURL)
	}()
	nousRefresher = shared.NewModelRefresher(time.Hour, nil, nil)

	applyConfig([]byte("inference_base_url: https://example.test/custom/v1\n"))
	if got := currentNousInferenceURL(); got != "https://example.test/custom/v1" {
		t.Fatalf("currentNousInferenceURL() = %q", got)
	}
	applyConfig([]byte("inference_base_url: https://example.test/custom/v1\n"))
	if got := currentNousInferenceURL(); got != "https://example.test/custom/v1" {
		t.Fatalf("idempotent endpoint update changed URL: %q", got)
	}
}

func TestAuthRefreshPreservesCatalogAndStoredEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token","expires_in":3600}`))
	}))
	defer server.Close()

	cache, err := json.Marshal([]rawCatalogModel{{ID: "cached/model:free"}})
	if err != nil {
		t.Fatal(err)
	}
	store := storageJSON{
		AccessToken:      "old-token",
		RefreshToken:     "refresh-token",
		PortalBaseURL:    server.URL,
		InferenceBaseURL: server.URL + "/v1",
		AccountID:        "account-a",
		ModelCatalog:     cache,
	}
	storage, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{
		"AuthID":      "account-a",
		"StorageJSON": storage,
	})
	if err != nil {
		t.Fatal(err)
	}

	response, _ := handleAuthRefresh(request)
	var envelope struct {
		Result struct {
			Auth struct {
				StorageJSON []byte `json:"StorageJSON"`
			} `json:"Auth"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	refreshed := decodeStorage(envelope.Result.Auth.StorageJSON)
	if refreshed.InferenceBaseURL != store.InferenceBaseURL {
		t.Fatalf("refresh changed stored endpoint to %q", refreshed.InferenceBaseURL)
	}
	if string(refreshed.ModelCatalog) != string(store.ModelCatalog) {
		t.Fatalf("refresh dropped model catalog: %q", refreshed.ModelCatalog)
	}
}

func TestAuthParsePreservesExpiredAccountIdentity(t *testing.T) {
	fileName := "nous-portal-free-2.json"
	storage, err := json.Marshal(map[string]any{
		"type":               ProviderID,
		"access_token":       "expired-token",
		"refresh_token":      "refresh-token",
		"expires_at":         time.Now().Add(-time.Hour),
		"inference_base_url": "https://example.test/v1",
		"account_id":         "account-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{"FileName": fileName, "RawJSON": storage})
	if err != nil {
		t.Fatal(err)
	}
	response, _ := handleAuthParse(request)
	var result struct {
		Result struct {
			Handled bool `json:"Handled"`
			Auth    struct {
				ID       string `json:"ID"`
				FileName string `json:"FileName"`
			} `json:"Auth"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Result.Handled || result.Result.Auth.ID != "account-2" || result.Result.Auth.FileName != fileName {
		t.Fatalf("unexpected expired auth parse: %s", response)
	}
}

func TestExpiredStorageRemainsLoadable(t *testing.T) {
	storage := storageJSON{
		AccessToken:      "expired",
		InferenceBaseURL: "https://example.test/v1",
		ExpiresAt:        time.Now().Add(-time.Minute),
	}
	if !storage.structuralValid() || !storage.valid() || storage.accessTokenUsable() {
		t.Fatalf("unexpected expired storage validity: %#v", storage)
	}
}

func TestFreeModelAllowedUsesFilteredSources(t *testing.T) {
	cache, err := json.Marshal([]rawCatalogModel{{ID: "cached/model:free"}, {ID: "paid/model"}})
	if err != nil {
		t.Fatal(err)
	}
	store := storageJSON{ModelCatalog: cache}
	if !freeModelAllowed(store, "cached/model:free") {
		t.Fatal("cached free model rejected")
	}
	if freeModelAllowed(store, "paid/model") {
		t.Fatal("paid model accepted")
	}
	if !freeModelAllowed(storageJSON{}, fallbackModels[0]) {
		t.Fatal("audited fallback model rejected")
	}
	if freeModelAllowed(storageJSON{}, "provider/paid") {
		t.Fatal("unlisted model accepted")
	}
}

func TestRegisterPayloadAdvertisesModelAliases(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal([]byte(registerPayload()), &root); err != nil {
		t.Fatal(err)
	}
	metadata := root["metadata"].(map[string]any)
	for _, raw := range metadata["ConfigFields"].([]any) {
		field := raw.(map[string]any)
		if field["Name"] == "model_aliases" && field["Type"] == "object" {
			return
		}
	}
	t.Fatal("model_aliases ConfigField missing")
}

func TestNousProbeHidesAndRestoresModel(t *testing.T) {
	status := http.StatusTooManyRequests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	cache, err := json.Marshal([]rawCatalogModel{{ID: "cached/model:free"}})
	if err != nil {
		t.Fatal(err)
	}
	store := storageJSON{
		AccessToken:      "token",
		InferenceBaseURL: server.URL,
		AccountID:        "account-probe",
		ModelCatalog:     cache,
	}
	scope := nousHealthScope(store)
	rememberNousProbeStore(store)
	defer func() {
		nousProbeStores.Lock()
		delete(nousProbeStores.byScope, scope)
		nousProbeStores.Unlock()
		modelHealth.RecordProbeSuccess(scope, "cached/model:free")
	}()
	target := shared.ModelProbeTarget{Scope: scope, Model: "cached/model:free"}
	if !modelHealth.BeginProbe(scope, target.Model) || probeNousModel(target) != shared.ProbeFailed {
		t.Fatal("failed Nous probe did not complete")
	}
	modelHealth.RecordProbeFailure(scope, target.Model)
	if !modelHealth.Hidden(scope, target.Model) {
		t.Fatal("failed Nous probe did not hide model")
	}

	status = http.StatusOK
	if !modelHealth.BeginProbe(scope, target.Model) || probeNousModel(target) != shared.ProbeSucceeded {
		t.Fatal("successful Nous recovery probe did not complete")
	}
	modelHealth.RecordProbeSuccess(scope, target.Model)
	if modelHealth.Hidden(scope, target.Model) {
		t.Fatal("successful Nous probe did not restore model")
	}
}

func TestAuthRefreshSingleflight(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer server.Close()

	storage, err := json.Marshal(storageJSON{
		AccessToken:      "expired-token",
		RefreshToken:     "old-refresh",
		ExpiresAt:        time.Now().Add(-time.Hour),
		PortalBaseURL:    server.URL,
		InferenceBaseURL: server.URL + "/v1",
		AccountID:        "singleflight-account",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{"StorageJSON": storage})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	responses := make([][]byte, 8)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Go(func() {
			<-start
			responses[i], _ = handleAuthRefresh(request)
		})
	}
	close(start)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh request did not reach upstream")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream refresh calls = %d, want 1", got)
	}
	close(release)
	wg.Wait()
	for i, response := range responses {
		if len(response) == 0 {
			t.Fatalf("response %d is empty", i)
		}
	}
}
