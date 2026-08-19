package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

var streamTransport = &http.Transport{
	ResponseHeaderTimeout: 30 * time.Second,
}

// handleExecutorExecute performs a non-streaming OpenAI chat-completions call.
func handleExecutorExecute(raw []byte) ([]byte, error) {
	var req struct {
		AuthProvider string `json:"AuthProvider"`
		Model        string `json:"Model"`
		Stream       bool   `json:"Stream"`
		Payload      []byte `json:"Payload"`
		StorageJSON  []byte `json:"StorageJSON"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorEnvelope("executor_execute_failed", "invalid request: "+err.Error()), nil
	}
	store := decodeStorage(req.StorageJSON)
	if !store.valid() {
		return errorEnvelopeWithStatus("auth_required", "nous-portal credential required", 401), nil
	}

	url := shared.TrimHTTP(store.InferenceBaseURL) + "/chat/completions"
	body, status, headers, err := doChatRequest(url, store.AccessToken, injectNousPortalTags(stripModelPrefixFromPayload(req.Payload)))
	if err != nil {
		return errorEnvelope("executor_execute_failed", err.Error()), nil
	}
	if status != 200 {
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+shared.Itoa(status)+": "+string(body), status), nil
	}
	return okEnvelopeJSON(mustJSON(map[string]any{
		"Payload": base64encode(body),
		"Headers": headerMap(headers),
	}))
}

// handleExecutorExecuteStream performs a streaming chat-completions call.
func handleExecutorExecuteStream(raw []byte) ([]byte, error) {
	var req struct {
		AuthProvider string `json:"AuthProvider"`
		Model        string `json:"Model"`
		Stream       bool   `json:"Stream"`
		Payload      []byte `json:"Payload"`
		StorageJSON  []byte `json:"StorageJSON"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorEnvelope("executor_stream_failed", "invalid request: "+err.Error()), nil
	}
	store := decodeStorage(req.StorageJSON)
	if !store.valid() {
		return errorEnvelopeWithStatus("auth_required", "nous-portal credential required", 401), nil
	}

	url := shared.TrimHTTP(store.InferenceBaseURL) + "/chat/completions"
	reader, status, headers, err := doChatStream(url, store.AccessToken, injectNousPortalTags(stripModelPrefixFromPayload(req.Payload)))
	if err != nil {
		return errorEnvelope("executor_stream_failed", err.Error()), nil
	}
	if status != 200 {
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, io.LimitReader(reader, 1<<20))
		_ = reader.Close()
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+shared.Itoa(status)+": "+buf.String(), status), nil
	}

	// Drain the SSE stream and encode each raw chunk as base64 for the host envelope.
	const (
		maxStreamChunks = 100000
		maxStreamBytes  = 100 * 1024 * 1024
	)
	chunks := make([]map[string]any, 0)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var totalBytes int
	for scanner.Scan() {
		if len(chunks) >= maxStreamChunks {
			_ = reader.Close()
			return errorEnvelope("executor_stream_failed", "stream exceeded max chunk limit"), nil
		}
		line := scanner.Text()
		if line == "" || line[0] == ':' {
			continue
		}
		line = strings.TrimPrefix(line, "data: ")
		totalBytes += len(line) + 1
		if totalBytes > maxStreamBytes {
			_ = reader.Close()
			return errorEnvelope("executor_stream_failed", "stream exceeded max byte limit"), nil
		}
		chunks = append(chunks, map[string]any{"Payload": []byte(line + "\n")})
	}
	if err := scanner.Err(); err != nil {
		_ = reader.Close()
		return errorEnvelope("executor_stream_failed", "stream read error: "+err.Error()), nil
	}
	_ = reader.Close()
	return okEnvelopeJSON(mustJSON(map[string]any{
		"Headers": headerMap(headers),
		"Chunks":  chunks,
	}))
}

// handleExecutorHTTPRequest bridges a raw HTTP request from the host through to the inference API.
func handleExecutorHTTPRequest(raw []byte) ([]byte, error) {
	var req struct {
		Method      string      `json:"Method"`
		URL         string      `json:"URL"`
		Headers     http.Header `json:"Headers"`
		Body        []byte      `json:"Body"`
		StorageJSON []byte      `json:"StorageJSON"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorEnvelope("executor_http_failed", "invalid request: "+err.Error()), nil
	}
	store := decodeStorage(req.StorageJSON)
	if !store.valid() {
		return errorEnvelopeWithStatus("auth_required", "nous-portal credential required", 401), nil
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	httpReq, err := http.NewRequest(method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return errorEnvelope("executor_http_failed", err.Error()), nil
	}
	copyHeaders(httpReq.Header, req.Headers)
	httpReq.Header.Set("Authorization", "Bearer "+store.AccessToken)
	if httpReq.Header.Get("Content-Type") == "" && len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return errorEnvelope("executor_http_failed", err.Error()), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return okEnvelopeJSON(mustJSON(map[string]any{
		"StatusCode": resp.StatusCode,
		"Headers":    headerMap(resp.Header),
		"Body":       base64encode(body),
	}))
}

// handleExecutorCountTokens forwards token counting to the inference API when available,
// otherwise returns a heuristic estimate.
func handleExecutorCountTokens(raw []byte) ([]byte, error) {
	var req struct {
		Model   string `json:"Model"`
		Payload []byte `json:"Payload"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorEnvelope("count_tokens_failed", "invalid request: "+err.Error()), nil
	}
	// Nous Portal has no documented token endpoint; estimate ~4 chars/token.
	n := len(req.Payload) / 4
	if n == 0 && len(req.Payload) > 0 {
		n = 1
	}
	return okEnvelopeJSON(mustJSON(map[string]any{
		"Payload": base64encode([]byte(fmt.Sprintf(`{"total_tokens":%d}`, n))),
	}))
}

// --- http transport helpers ---

// injectNousPortalTags ensures the upstream Nous Portal request carries the
// expected Hermes product-attribution tags. If the host already supplied tags,
// we leave them alone.
func injectNousPortalTags(payload []byte) []byte {
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return payload
	}
	if _, ok := root["tags"]; ok {
		return payload
	}
	root["tags"] = []string{
		"product=hermes-agent",
		"client=hermes-client-v0.20.1",
	}
	out, err := json.Marshal(root)
	if err != nil {
		return payload
	}
	return out
}

// doChatRequest performs a non-streaming OpenAI chat-completions call to Nous Portal.
func doChatRequest(url, apiKey string, payload []byte) ([]byte, int, http.Header, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "hermes-cli/0.20.1")
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	return body, resp.StatusCode, resp.Header, nil
}

// doChatStream performs a streaming OpenAI chat-completions call to Nous Portal.
func doChatStream(url, apiKey string, payload []byte) (io.ReadCloser, int, http.Header, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "hermes-cli/0.20.1")
	client := &http.Client{Transport: streamTransport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	if resp.StatusCode != 200 {
		return resp.Body, resp.StatusCode, resp.Header, nil
	}
	return resp.Body, resp.StatusCode, resp.Header, nil
}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

func headerMap(h http.Header) map[string][]string {
	if h == nil {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(h))
	for k, v := range h {
		if len(v) == 0 {
			continue
		}
		lower := strings.ToLower(k)
		if lower == "authorization" || lower == "cookie" || lower == "set-cookie" {
			continue
		}
		out[k] = v
	}
	return out
}
