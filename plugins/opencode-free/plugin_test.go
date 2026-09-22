package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func TestModelForAuthRefreshesEmptyCatalog(t *testing.T) {
	original := opencodeRefresher
	defer func() { opencodeRefresher = original }()
	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"example-free"}, nil
	}, nil)

	response, _ := handleModelForAuth([]byte(`{"AuthID":"test-auth"}`))
	if !strings.Contains(string(response), "example-free") {
		t.Fatalf("model.for_auth response does not contain refreshed model: %s", response)
	}
}

func TestModelForAuthReturnsRefreshErrorWhenCatalogUnavailable(t *testing.T) {
	original := opencodeRefresher
	defer func() { opencodeRefresher = original }()
	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return nil, errors.New("upstream unavailable")
	}, nil)

	response, _ := handleModelForAuth([]byte(`{"AuthID":"test-auth"}`))
	if !strings.Contains(string(response), `"model_refresh_failed"`) {
		t.Fatalf("expected model refresh error, got: %s", response)
	}
}

func TestConfigEndpoints(t *testing.T) {
	cfg := config{BaseURL: "https://example.test/zen"}
	chat, models := cfg.endpoints()
	if chat != "https://example.test/zen/v1/chat/completions" || models != "https://example.test/zen/v1/models" {
		t.Fatalf("base endpoints = %q, %q", chat, models)
	}

	cfg.ChatURL = "https://chat.example.test/completions"
	cfg.ModelsURL = "https://models.example.test/list"
	chat, models = cfg.endpoints()
	if chat != cfg.ChatURL || models != cfg.ModelsURL {
		t.Fatalf("explicit endpoints = %q, %q", chat, models)
	}
}

func TestExecutorRefreshesBeforeRequest(t *testing.T) {
	originalRefresher := opencodeRefresher
	originalChatURL := currentOpenCodeChatURL()
	defer func() {
		opencodeRefresher = originalRefresher
		endpointMu.Lock()
		opencodeChatURL = originalChatURL
		endpointMu.Unlock()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"example-free"}, nil
	}, nil)
	endpointMu.Lock()
	opencodeChatURL = server.URL
	endpointMu.Unlock()

	response, _ := handleExecutorExecute([]byte(`{"Model":"example-free","Messages":[{"role":"user","content":"hi"}]}`))
	if !strings.Contains(string(response), `"Payload":"`) {
		t.Fatalf("executor did not refresh and execute: %s", response)
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

func TestExecutorCountTokens(t *testing.T) {
	response, _ := handleExecutorCountTokens([]byte(`{"Model":"example-free","prompt":"12345678"}`))
	if !strings.Contains(string(response), `"Count":2`) {
		t.Fatalf("unexpected count_tokens response: %s", response)
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
	expected := "opencode-free"
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
	got := prefixedModelID("deepseek-v4-flash-free")
	if got != "opencode-free/deepseek-v4-flash-free" {
		t.Errorf("prefixedModelID() = %q, want %q", got, "opencode-free/deepseek-v4-flash-free")
	}
}

func TestStripModelPrefix(t *testing.T) {
	got := stripModelPrefix("opencode-free/deepseek-v4-flash-free")
	if got != "deepseek-v4-flash-free" {
		t.Errorf("stripModelPrefix() = %q, want %q", got, "deepseek-v4-flash-free")
	}
}

func TestResolveConfigYAML(t *testing.T) {
	cfg := resolveConfig([]byte("enabled: true\nmodel_aliases:\n    ox-alpha: x-preview-f-free\n"))
	if cfg.ModelAliases["ox-alpha"] != "x-preview-f-free" {
		t.Fatalf("model_aliases not parsed from YAML: %v", cfg.ModelAliases)
	}
}

func TestExecutorStreamForcesSSE(t *testing.T) {
	originalRefresher := opencodeRefresher
	originalChatURL := currentOpenCodeChatURL()
	defer func() {
		opencodeRefresher = originalRefresher
		endpointMu.Lock()
		opencodeChatURL = originalChatURL
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
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"example-free"}, nil
	}, nil)
	endpointMu.Lock()
	opencodeChatURL = server.URL
	endpointMu.Unlock()

	response, _ := handleExecutorExecuteStream([]byte(`{"Model":"example-free","Messages":[]}`))
	if !strings.Contains(string(response), `"Chunks"`) || accept != "text/event-stream" || !streamValue {
		t.Fatalf("stream request not normalized: response=%s accept=%q stream=%v", response, accept, streamValue)
	}
}

func TestExecutorStreamRejectsIncompleteStream(t *testing.T) {
	setOpencodeHealthChecksEnabled(true)
	defer setOpencodeHealthChecksEnabled(false)
	originalRefresher := opencodeRefresher
	originalChatURL := currentOpenCodeChatURL()
	defer func() {
		opencodeRefresher = originalRefresher
		endpointMu.Lock()
		opencodeChatURL = originalChatURL
		endpointMu.Unlock()
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	}))
	defer server.Close()
	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"truncated-free"}, nil
	}, nil)
	endpointMu.Lock()
	opencodeChatURL = server.URL
	endpointMu.Unlock()

	response, _ := handleExecutorExecuteStream([]byte(`{"Model":"truncated-free","Messages":[]}`))
	if !strings.Contains(string(response), "incomplete chat stream") {
		t.Fatalf("truncated stream was not rejected: %s", response)
	}
	if !modelHealth.Hidden(openCodeHealthScope(), "truncated-free") {
		t.Fatal("truncated stream did not mark the model unhealthy")
	}
	modelHealth.RecordProbeSuccess(openCodeHealthScope(), "truncated-free")
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

func TestModelStaticHidesAndRestoresProbeFailure(t *testing.T) {
	setOpencodeHealthChecksEnabled(true)
	defer setOpencodeHealthChecksEnabled(false)
	originalRefresher := opencodeRefresher
	defer func() { opencodeRefresher = originalRefresher }()
	model := "probe-failure-free"
	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{model}, nil
	}, nil)
	scope := openCodeHealthScope()
	modelHealth.RecordProbeFailure(scope, model)
	defer modelHealth.RecordProbeSuccess(scope, model)

	response, _ := handleModelStatic(nil)
	var hidden struct {
		Result struct {
			Models []map[string]any `json:"Models"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &hidden); err != nil {
		t.Fatal(err)
	}
	if len(hidden.Result.Models) != 0 {
		t.Fatalf("quarantined model remained visible: %s", response)
	}

	modelHealth.RecordProbeSuccess(scope, model)
	response, _ = handleModelStatic(nil)
	if err := json.Unmarshal(response, &hidden); err != nil {
		t.Fatal(err)
	}
	if len(hidden.Result.Models) != 1 {
		t.Fatalf("recovered model was not visible: %s", response)
	}
}

func TestSmokeFailureHidesAndProbeRestoresModel(t *testing.T) {
	setOpencodeHealthChecksEnabled(true)
	defer setOpencodeHealthChecksEnabled(false)
	originalRefresher := opencodeRefresher
	originalChatURL := currentOpenCodeChatURL()
	defer func() {
		opencodeRefresher = originalRefresher
		endpointMu.Lock()
		opencodeChatURL = originalChatURL
		endpointMu.Unlock()
	}()
	model := "probe-health-free"
	status := http.StatusServiceUnavailable
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{model}, nil
	}, nil)
	endpointMu.Lock()
	opencodeChatURL = server.URL
	endpointMu.Unlock()

	response, _ := handleExecutorExecute([]byte(`{"Model":"probe-health-free","Messages":[{"role":"user","content":"hi"}]}`))
	if !strings.Contains(string(response), `"upstream_error"`) {
		t.Fatalf("smoke failure response = %s", response)
	}
	scope := openCodeHealthScope()
	if !modelHealth.Hidden(scope, model) {
		t.Fatal("unavailable model remained visible")
	}

	status = http.StatusOK
	target := shared.ModelProbeTarget{Scope: scope, Model: model}
	if !modelHealth.BeginProbe(scope, model) || probeOpenCodeModel(target) != shared.ProbeSucceeded {
		t.Fatal("successful recovery probe did not complete")
	}
	modelHealth.RecordProbeSuccess(scope, model)
	response, _ = handleModelStatic(nil)
	if strings.Contains(string(response), `"Models":[]`) {
		t.Fatalf("recovered model remained hidden: %s", response)
	}
	modelHealth.RecordProbeSuccess(scope, model)
}

func TestFetchOpenCodeMetadataFallsBackToProviderNPM(t *testing.T) {
	originalURL := openCodeMetadataURL
	originalClient := httpClient
	defer func() {
		openCodeMetadataURL = originalURL
		httpClient = originalClient
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "opencode": {"id":"opencode", "npm":"@ai-sdk/openai-compatible", "models":{
    "chat-free":{"cost":{"input":0,"output":0}},
    "vision-free":{"cost":{"input":0,"output":0},"provider":{"npm":"@ai-sdk/openai"}}
  }}
}`))
	}))
	defer server.Close()
	openCodeMetadataURL = server.URL
	httpClient = server.Client()

	metadata, err := fetchOpenCodeMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if got := metadata["chat-free"].Provider.NPM; got != "@ai-sdk/openai-compatible" {
		t.Fatalf("provider npm fallback missing: %q", got)
	}
	if got := metadata["vision-free"].Provider.NPM; got != "@ai-sdk/openai" {
		t.Fatalf("model-level npm was overwritten: %q", got)
	}
}

func TestFetchOpenCodeModelsFiltersByMetadata(t *testing.T) {
	originalMetadataURL := openCodeMetadataURL
	originalClient := httpClient
	originalModelsURL := currentOpenCodeModelsURL()
	defer func() {
		openCodeMetadataURL = originalMetadataURL
		httpClient = originalClient
		endpointMu.Lock()
		opencodeModelsURL = originalModelsURL
		endpointMu.Unlock()
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/metadata" {
			_, _ = w.Write([]byte(`{"opencode":{"id":"opencode","npm":"@ai-sdk/openai-compatible","models":{
  "chat-free":{"cost":{"input":0,"output":0}},
  "responses-free":{"cost":{"input":0,"output":0},"provider":{"npm":"@ai-sdk/anthropic"}},
  "paid-free":{"cost":{"input":1,"output":2}},
  "retired-free":{"cost":{"input":0,"output":0},"status":"deprecated"}
}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"chat-free"},{"id":"responses-free"},{"id":"paid-free"},{"id":"retired-free"},{"id":"unlisted-free"}]}`))
	}))
	defer server.Close()
	openCodeMetadataURL = server.URL + "/metadata"
	httpClient = server.Client()
	endpointMu.Lock()
	opencodeModelsURL = server.URL + "/v1/models"
	endpointMu.Unlock()

	ids, err := fetchOpenCodeModels()
	if err != nil {
		t.Fatal(err)
	}
	// Unlisted models have no official metadata and must be dropped rather than
	// guessed as free.
	if got := strings.Join(ids, ","); got != "chat-free" {
		t.Fatalf("metadata-filtered catalog = %q", got)
	}
}

func TestModelEntryUsesMetadataCapabilities(t *testing.T) {
	originalCatalog := openCodeCatalog
	defer func() {
		openCodeCatalogMu.Lock()
		openCodeCatalog = originalCatalog
		openCodeCatalogMu.Unlock()
	}()
	metadata := openCodeModelMetadata{Name: "Bounded Model", Description: "desc", Reasoning: true, ToolCall: true}
	metadata.Limit.Context = 1000000
	metadata.Limit.Output = 128000
	metadata.Modalities.Input = []string{"text", "image"}
	openCodeCatalogMu.Lock()
	openCodeCatalog = []openCodeCatalogModel{{
		ID:       "bounded-free",
		Created:  1700000000,
		OwnedBy:  "opencode",
		Metadata: metadata,
	}}
	openCodeCatalogMu.Unlock()

	entry := modelEntry("bounded-free", "bounded-free")
	if entry["ContextLength"] != 1000000 || entry["MaxCompletionTokens"] != 128000 {
		t.Fatalf("metadata limits not applied: %v", entry)
	}
	if entry["DisplayName"] != "Bounded Model" || entry["Reasoning"] != true || entry["ToolCall"] != true {
		t.Fatalf("metadata capabilities not applied: %v", entry)
	}
	if got, ok := entry["SupportedInputModalities"].([]string); !ok || len(got) != 2 {
		t.Fatalf("metadata modalities not applied: %v", entry["SupportedInputModalities"])
	}
	if entry["Created"] != int64(1700000000) || entry["OwnedBy"] != "opencode" {
		t.Fatalf("live fields not applied: %v", entry)
	}

	// An alias inherits its target metadata; an unknown ID keeps identity only.
	alias := modelEntry("deep-free", "bounded-free")
	if alias["ContextLength"] != 1000000 {
		t.Fatalf("alias did not inherit target metadata: %v", alias)
	}
	unknown := modelEntry("unknown-free", "unknown-free")
	if _, ok := unknown["ContextLength"]; ok {
		t.Fatalf("unknown model reported a context length: %v", unknown)
	}
}
func TestExecutorFramingAndHeaders(t *testing.T) {
	originalRefresher := opencodeRefresher
	originalChatURL := currentOpenCodeChatURL()
	defer func() {
		opencodeRefresher = originalRefresher
		endpointMu.Lock()
		opencodeChatURL = originalChatURL
		endpointMu.Unlock()
	}()

	var capturedReq map[string]any
	var userAgent, clientHdr, sessionHdr, requestHdr string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		clientHdr = r.Header.Get("x-opencode-client")
		sessionHdr = r.Header.Get("x-opencode-session")
		requestHdr = r.Header.Get("x-opencode-request")

		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"framing-model-free"}, nil
	}, nil)
	endpointMu.Lock()
	opencodeChatURL = server.URL
	endpointMu.Unlock()

	resp, err := handleExecutorExecute([]byte(`{"Model":"framing-model-free","Messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("handleExecutorExecute failed: %v", err)
	}
	if !strings.Contains(string(resp), `"Payload":"`) {
		t.Fatalf("unexpected response: %s", resp)
	}

	if userAgent != "opencode/1.18.32" {
		t.Errorf("expected User-Agent opencode/1.18.32, got %q", userAgent)
	}
	if clientHdr != "cli" {
		t.Errorf("expected x-opencode-client cli, got %q", clientHdr)
	}
	if !strings.HasPrefix(sessionHdr, "ses_") {
		t.Errorf("expected x-opencode-session to start with ses_, got %q", sessionHdr)
	}
	if !strings.HasPrefix(requestHdr, "msg_") {
		t.Errorf("expected x-opencode-request to start with msg_, got %q", requestHdr)
	}

	if stream, ok := capturedReq["stream"].(bool); !ok || !stream {
		t.Errorf("expected stream: true in captured request")
	}
	tools, ok := capturedReq["tools"].([]any)
	if !ok || len(tools) < 2 {
		t.Fatalf("expected injected minimal tools, got: %v", capturedReq["tools"])
	}
	if tc, ok := capturedReq["tool_choice"].(string); !ok || tc != "none" {
		t.Errorf("expected tool_choice: 'none', got %v", capturedReq["tool_choice"])
	}
}

func TestExecutorResponsesModelRouting(t *testing.T) {
	originalChatURL := currentOpenCodeChatURL()
	defer func() {
		endpointMu.Lock()
		opencodeChatURL = originalChatURL
		endpointMu.Unlock()
	}()

	var calledResponses bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			calledResponses = true
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"created_at\":1700000000,\"model\":\"muse-spark-1.3-contributor-free\"}}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"created_at\":1700000000,\"model\":\"muse-spark-1.3-contributor-free\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n")
			return
		}
		http.Error(w, "unexpected path", http.StatusNotFound)
	}))
	defer server.Close()

	opencodeRefresher = shared.NewModelRefresher(time.Hour, func() ([]string, error) {
		return []string{"muse-spark-1.3-contributor-free"}, nil
	}, nil)
	endpointMu.Lock()
	opencodeChatURL = server.URL + "/v1/chat/completions"
	endpointMu.Unlock()

	resp, err := handleExecutorExecute([]byte(`{"Model":"muse-spark-1.3-contributor-free","Messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("handleExecutorExecute failed: %v", err)
	}
	t.Logf("resp: %s", string(resp))
	if !calledResponses {
		t.Errorf("expected request to route to /responses endpoint")
	}
	var hostResp struct {
		Result struct {
			Payload string `json:"Payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &hostResp); err != nil {
		t.Fatalf("failed to unmarshal host envelope: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(hostResp.Result.Payload)
	if err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	var chatResp map[string]any
	if err := json.Unmarshal(decoded, &chatResp); err != nil {
		t.Fatalf("failed to unmarshal chat response (%q): %v", string(decoded), err)
	}
	choices := chatResp["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "OK" {
		t.Errorf("expected content 'OK', got %v", msg["content"])
	}
}
