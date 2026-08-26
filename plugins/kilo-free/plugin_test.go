package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func TestModelForAuthRefreshesEmptyCatalog(t *testing.T) {
	original := kiloRefresher
	defer func() { kiloRefresher = original }()
	kiloRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"example-free"}, nil
	}, nil)

	response, _ := handleModelForAuth([]byte(`{"AuthID":"test-auth"}`))
	if !strings.Contains(string(response), "example-free") {
		t.Fatalf("model.for_auth response does not contain refreshed model: %s", response)
	}
}

func TestModelForAuthRejectsInvalidJSON(t *testing.T) {
	response, _ := handleModelForAuth([]byte("{"))
	if !strings.Contains(string(response), `"invalid_request"`) {
		t.Fatalf("expected invalid request error, got: %s", response)
	}
}

func TestModelForAuthReturnsRefreshErrorWhenCatalogUnavailable(t *testing.T) {
	original := kiloRefresher
	defer func() { kiloRefresher = original }()
	kiloRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return nil, errors.New("upstream unavailable")
	}, nil)

	response, _ := handleModelForAuth([]byte(`{"AuthID":"test-auth"}`))
	if !strings.Contains(string(response), `"model_refresh_failed"`) {
		t.Fatalf("expected model refresh error, got: %s", response)
	}
}

func TestConfigEndpoints(t *testing.T) {
	cfg := config{BaseURL: "https://example.test/gateway"}
	chat, models := cfg.endpoints()
	if chat != "https://example.test/gateway/v1/chat/completions" {
		t.Fatalf("chat endpoint = %q", chat)
	}
	if models != "https://example.test/gateway/models" {
		t.Fatalf("models endpoint = %q", models)
	}

	cfg.ChatURL = "https://chat.example.test/completions"
	cfg.ModelsURL = "https://models.example.test/list"
	chat, models = cfg.endpoints()
	if chat != cfg.ChatURL || models != cfg.ModelsURL {
		t.Fatalf("explicit endpoints = %q, %q", chat, models)
	}
}

func TestExecutorRefreshesBeforeRequest(t *testing.T) {
	originalRefresher := kiloRefresher
	originalChatURL := currentKiloChatURL()
	defer func() {
		kiloRefresher = originalRefresher
		endpointMu.Lock()
		kiloChatURL = originalChatURL
		endpointMu.Unlock()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	kiloRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"example-free"}, nil
	}, nil)
	endpointMu.Lock()
	kiloChatURL = server.URL
	endpointMu.Unlock()

	response, _ := handleExecutorExecute([]byte(`{"Model":"example-free","Messages":[{"role":"user","content":"hi"}]}`))
	if !strings.Contains(string(response), `"Payload":"`) {
		t.Fatalf("executor did not refresh and execute: %s", response)
	}
}

func TestExecutorCountTokens(t *testing.T) {
	response, _ := handleExecutorCountTokens([]byte(`{"Model":"example-free","prompt":"12345678"}`))
	if !strings.Contains(string(response), `"Count":2`) {
		t.Fatalf("unexpected count_tokens response: %s", response)
	}
}

func TestExecutorHTTPRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" {
			t.Errorf("missing request header")
		}
		w.Header().Set("X-Response", "yes")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	response, _ := handleExecutorHTTPRequest([]byte(`{"method":"GET","url":"` + server.URL + `","headers":{"X-Test":"yes"}}`))
	if !strings.Contains(string(response), `"StatusCode":200`) || !strings.Contains(string(response), `"X-Response":["yes"]`) {
		t.Fatalf("unexpected http_request response: %s", response)
	}
	var envelope struct {
		Result struct {
			Body string `json:"Body"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	body, err := base64.StdEncoding.DecodeString(envelope.Result.Body)
	if err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("unexpected response body: %q (%v)", body, err)
	}
}

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
	expected := "kilo-free"
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
	if got != "kilo-free/tencent/hy3:free" {
		t.Errorf("prefixedModelID() = %q, want %q", got, "kilo-free/tencent/hy3:free")
	}
}

func TestStripModelPrefix(t *testing.T) {
	got := stripModelPrefix("kilo-free/tencent/hy3:free")
	if got != "tencent/hy3:free" {
		t.Errorf("stripModelPrefix() = %q, want %q", got, "tencent/hy3:free")
	}
}

func TestExecutorStreamForcesSSE(t *testing.T) {
	originalRefresher := kiloRefresher
	originalChatURL := currentKiloChatURL()
	defer func() {
		kiloRefresher = originalRefresher
		endpointMu.Lock()
		kiloChatURL = originalChatURL
		endpointMu.Unlock()
	}()
	var streamValue bool
	var accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		streamValue, _ = payload["stream"].(bool)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer server.Close()
	kiloRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"example-free"}, nil
	}, nil)
	endpointMu.Lock()
	kiloChatURL = server.URL
	endpointMu.Unlock()

	response, _ := handleExecutorExecuteStream([]byte(`{"Model":"example-free","Messages":[]}`))
	if !strings.Contains(string(response), `"Chunks"`) || accept != "text/event-stream" || !streamValue {
		t.Fatalf("stream request not normalized: response=%s accept=%q stream=%v", response, accept, streamValue)
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
