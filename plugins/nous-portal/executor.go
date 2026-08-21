package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

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
	body, status, headers, err := shared.DoChatRequest(url, store.AccessToken, shared.InjectNousPortalTags(resolveModelFromPayload(req.Payload)))
	if err != nil {
		return errorEnvelope("executor_execute_failed", err.Error()), nil
	}
	if status != 200 {
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+strconv.Itoa(status)+": "+string(body), status), nil
	}
	return okEnvelopeJSON(mustJSON(map[string]any{
		"Payload": base64encode(body),
		"Headers": shared.HeaderMap(headers),
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
	reader, status, headers, err := shared.DoChatStream(url, store.AccessToken, shared.InjectNousPortalTags(resolveModelFromPayload(req.Payload)))
	if err != nil {
		return errorEnvelope("executor_stream_failed", err.Error()), nil
	}
	if status != 200 {
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, io.LimitReader(reader, 1<<20))
		_ = reader.Close()
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+strconv.Itoa(status)+": "+buf.String(), status), nil
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
		"Headers": shared.HeaderMap(headers),
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
	shared.CopyHeaders(httpReq.Header, req.Headers)
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
		"Headers":    shared.HeaderMap(resp.Header),
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
