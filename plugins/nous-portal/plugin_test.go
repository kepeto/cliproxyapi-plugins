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
	// Default prefix should be the plugin ID
	expected := "nous-portal"
	if currentPrefix() != expected {
		t.Errorf("currentPrefix() = %q, want %q", currentPrefix(), expected)
	}
}

func TestPluginPrefixOverride(t *testing.T) {
	// Save original prefix
	orig := currentPrefix()

	// Override prefix
	setPluginPrefix("custom-prefix")
	if currentPrefix() != "custom-prefix" {
		t.Errorf("currentPrefix() = %q, want %q", currentPrefix(), "custom-prefix")
	}

	// Verify prefixedModelID uses new prefix
	got := prefixedModelID("tencent/hy3:free")
	if got != "custom-prefix/tencent/hy3:free" {
		t.Errorf("prefixedModelID() = %q, want %q", got, "custom-prefix/tencent/hy3:free")
	}

	// Verify stripModelPrefix uses new prefix
	got = stripModelPrefix("custom-prefix/tencent/hy3:free")
	if got != "tencent/hy3:free" {
		t.Errorf("stripModelPrefix() = %q, want %q", got, "tencent/hy3:free")
	}

	// Restore original prefix
	setPluginPrefix(orig)
}

func TestPrefixedModelID(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"tencent/hy3:free", "nous-portal/tencent/hy3:free"},
		{"", ""},
	}

	for _, tt := range tests {
		got := prefixedModelID(tt.modelID)
		if got != tt.expected {
			t.Errorf("prefixedModelID(%q) = %q, want %q", tt.modelID, got, tt.expected)
		}
	}
}

func TestStripModelPrefix(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"nous-portal/tencent/hy3:free", "tencent/hy3:free"},
		{"tencent/hy3:free", "tencent/hy3:free"},
		{"", ""},
	}

	for _, tt := range tests {
		got := stripModelPrefix(tt.modelID)
		if got != tt.expected {
			t.Errorf("stripModelPrefix(%q) = %q, want %q", tt.modelID, got, tt.expected)
		}
	}
}

func TestPrefixSetAndGet(t *testing.T) {
	p := shared.NewPrefix("test")
	if p.Value() != "test" {
		t.Errorf("Prefix.Value() = %q, want %q", p.Value(), "test")
	}

	p.Set("new-test")
	if p.Value() != "new-test" {
		t.Errorf("Prefix.Value() after Set() = %q, want %q", p.Value(), "new-test")
	}

	got := p.Prefixed("model")
	if got != "new-test/model" {
		t.Errorf("Prefix.Prefixed() = %q, want %q", got, "new-test/model")
	}
}

func TestAuthParsePreservesExpiredAccountIdentity(t *testing.T) {
	fileName := "nous-portal-2.json"
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

func TestLoginPollUsesConfiguredEndpointFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
	}))
	defer server.Close()
	state := "login-state"
	loginStates.put(state, &loginState{
		deviceCode:       "device",
		expiresAt:        time.Now().Add(time.Hour),
		interval:         1,
		portalBaseURL:    server.URL,
		inferenceBaseURL: server.URL + "/configured/v1",
		clientID:         "client",
		scope:            "scope",
		accountFileName:  "nous-portal.json",
	})
	request, err := json.Marshal(map[string]any{"State": state})
	if err != nil {
		t.Fatal(err)
	}
	response, _ := handleAuthLoginPoll(request)
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
	store := decodeStorage(envelope.Result.Auth.StorageJSON)
	if store.InferenceBaseURL != server.URL+"/configured/v1" {
		t.Fatalf("inference endpoint = %q", store.InferenceBaseURL)
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
