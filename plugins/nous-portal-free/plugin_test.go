package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFetchPortalFreeModelsMatchesHermesList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/nous/recommended-models" {
			t.Errorf("unexpected portal path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"freeRecommendedModels":[{"modelName":"upstage/solar-pro4:free"},{"modelName":"poolside/laguna-s-2.1:free"},{"modelName":"upstage/solar-pro4:free"},{"modelName":""}]}`))
	}))
	defer server.Close()
	got := fetchPortalFreeModels(server.URL)
	if len(got) != 2 || got[0].ID != "upstage/solar-pro4:free" || got[1].ID != "poolside/laguna-s-2.1:free" {
		t.Fatalf("unexpected portal free models: %#v", got)
	}
}

func TestUnionFreeModelsDeduplicates(t *testing.T) {
	merged := unionFreeModels(
		[]rawCatalogModel{{ID: "upstage/solar-pro4:free"}, {ID: ""}},
		[]rawCatalogModel{{ID: "upstage/solar-pro4:free"}, {ID: "tencent/hy3:free"}},
	)
	if len(merged) != 2 || merged[0].ID != "upstage/solar-pro4:free" || merged[1].ID != "tencent/hy3:free" {
		t.Fatalf("unexpected union: %#v", merged)
	}
}

func TestModelForAuthUnionsPortalAndInferenceCatalogs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/nous/recommended-models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"freeRecommendedModels":[{"modelName":"poolside/laguna-s-2.1:free"}]}`))
	})
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"tencent/hy3:free","name":"hy3"}]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	storage, err := json.Marshal(storageJSON{
		AccessToken:      "token",
		PortalBaseURL:    server.URL,
		InferenceBaseURL: server.URL,
		AccountID:        "union-account",
	})
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
			Models []map[string]any `json:"Models"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Result.Models) != 2 {
		t.Fatalf("expected portal+inference union, got: %s", response)
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
		PortalBaseURL:    server.URL,
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
	originalPortal := currentNousPortalURL()
	originalRefresher := nousRefresher
	defer func() {
		nousRefresher = originalRefresher
		setNousInferenceURL(originalURL)
		setNousPortalURL(originalPortal)
	}()
	nousRefresher = shared.NewModelRefresher(time.Hour, nil, nil)

	applyConfig([]byte("inference_base_url: https://example.test/custom/v1\nportal_base_url: https://portal.example.test\n"))
	if got := currentNousInferenceURL(); got != "https://example.test/custom/v1" {
		t.Fatalf("currentNousInferenceURL() = %q", got)
	}
	if got := currentNousPortalURL(); got != "https://portal.example.test" {
		t.Fatalf("currentNousPortalURL() = %q", got)
	}
	applyConfig([]byte("inference_base_url: https://example.test/custom/v1\nportal_base_url: https://portal.example.test\n"))
	if got := currentNousInferenceURL(); got != "https://example.test/custom/v1" {
		t.Fatalf("idempotent endpoint update changed URL: %q", got)
	}
}

func TestAuthParseExposesEmailAndQuota(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"user@example.test","subscription_tier":"free","member_spend_usd":0.42,"member_spend_cap_usd":5,"member_spend_cap_remaining_usd":4.58}`))
	storage, err := json.Marshal(map[string]any{
		"type":               ProviderID,
		"access_token":       "header." + payload + ".signature",
		"refresh_token":      "refresh-token",
		"inference_base_url": "https://example.test/v1",
		"account_id":         "account-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{"FileName": "nous-portal-free.json", "RawJSON": storage})
	if err != nil {
		t.Fatal(err)
	}
	response, _ := handleAuthParse(request)
	var result struct {
		Result struct {
			Handled bool `json:"Handled"`
			Auth    struct {
				Label    string         `json:"Label"`
				Metadata map[string]any `json:"Metadata"`
			} `json:"Auth"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Result.Handled || !strings.Contains(result.Result.Auth.Label, "user@example.test") {
		t.Fatalf("identity missing from label: %s", response)
	}
	meta := result.Result.Auth.Metadata
	if meta["username"] != "user@example.test" || meta["spend_usd"] != "0.42" || meta["spend_remaining_usd"] != "4.58" {
		t.Fatalf("quota metadata incorrect: %s", response)
	}
}

func TestLoginPollUnknownOAuthErrorIsTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"invalid client"}`))
	}))
	defer server.Close()

	state := "terminal-oauth-error"
	loginStates.put(state, &loginState{
		deviceCode:    "device",
		expiresAt:     time.Now().Add(time.Hour),
		interval:      1,
		portalBaseURL: server.URL,
		clientID:      "client",
		scope:         "scope",
	})
	request, err := json.Marshal(map[string]any{"State": state})
	if err != nil {
		t.Fatal(err)
	}
	response, _ := handleAuthLoginPoll(request)
	var envelope struct {
		Result struct {
			Status string `json:"Status"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Result.Status != "error" {
		t.Fatalf("unknown OAuth error remained pending: %s", response)
	}
	if _, ok := loginStates.get(state); ok {
		t.Fatal("terminal OAuth error left login state active")
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

func TestAuthRefreshUsesDocumentedHeaderAndRotatesToken(t *testing.T) {
	var gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Nous-Refresh-Token")
		if r.FormValue("refresh_token") != "" {
			t.Errorf("refresh token leaked into form body")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-token","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer server.Close()

	storage, err := json.Marshal(storageJSON{
		AccessToken:      "old-token",
		RefreshToken:     "old-refresh",
		PortalBaseURL:    server.URL,
		InferenceBaseURL: server.URL + "/v1",
		AccountID:        "header-account",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{"StorageJSON": storage})
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
	if gotHeader != "old-refresh" || refreshed.AccessToken != "new-token" || refreshed.RefreshToken != "new-refresh" {
		t.Fatalf("refresh header/token mapping incorrect: header=%q storage=%#v response=%s", gotHeader, refreshed, response)
	}
}

func TestAuthRefreshRejectedTokenRequiresReauthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"refresh_token_reused"}`))
	}))
	defer server.Close()

	storage, err := json.Marshal(storageJSON{
		AccessToken:      "old-token",
		RefreshToken:     "reused-refresh",
		PortalBaseURL:    server.URL,
		InferenceBaseURL: server.URL + "/v1",
		AccountID:        "reused-account",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(map[string]any{"StorageJSON": storage})
	if err != nil {
		t.Fatal(err)
	}
	response, _ := handleAuthRefresh(request)
	var envelope struct {
		Error struct {
			Code       string `json:"code"`
			HTTPStatus int    `json:"http_status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "reauth_required" || envelope.Error.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("unexpected rejected-token response: %s", response)
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
