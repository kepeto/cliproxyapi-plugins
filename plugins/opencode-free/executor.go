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

func recordInferenceFailure(scope, model string, status int, body []byte, err error) {
	if status == 401 || status == 403 {
		return
	}
	if err != nil || status == 408 || status == 429 || status >= 500 {
		modelHealth.RecordProbeFailure(scope, model)
		return
	}
	if shared.IsModelSpecificFailure(status, body, nil) {
		modelHealth.RecordFailure(scope, model)
	}
}

var streamTransport = &http.Transport{
	ResponseHeaderTimeout: 30 * time.Second,
}

func handleExecutorIdentifier() ([]byte, error) {
	return shared.OKEnvelope(`{"Identifier":"` + EXECUTOR_ID + `"}`)
}

func handleExecutorExecute(rawReq []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}

	modelID, _ := req["Model"].(string)
	if modelID == "" {
		return errorEnvelope("invalid_request", "missing model"), nil
	}
	if err := ensureModels(); err != nil {
		return errorEnvelope("model_refresh_failed", err.Error()), nil
	}

	baseModelID := resolveModel(modelID)
	if baseModelID != "" && !opencodeRefresher.Contains(baseModelID) {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}
	if !modelHealth.Allow(openCodeHealthScope(), baseModelID) {
		return errorEnvelope("model_quarantined", fmt.Sprintf("model %q is temporarily unavailable", modelID)), nil
	}

	openaiReq := shared.ConvertToOpenAI(req, baseModelID, resolveModel)
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	status, body, err := executeOpenCodeChatWithRetry(payload, false)
	if err != nil {
		recordInferenceFailure(openCodeHealthScope(), baseModelID, status, body, err)
		return errorEnvelope("upstream_error", err.Error()), nil
	}

	if status < 200 || status >= 300 {
		recordInferenceFailure(openCodeHealthScope(), baseModelID, status, body, nil)
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+strconv.Itoa(status)+": "+string(body), status), nil
	}
	if !shared.ValidChatResponse(body) {
		modelHealth.RecordProbeFailure(openCodeHealthScope(), baseModelID)
		return errorEnvelope("upstream_error", "invalid or empty chat response"), nil
	}
	modelHealth.RecordSuccess(openCodeHealthScope(), baseModelID)
	return okEnvelopeJSON(shared.MustJSON(map[string]interface{}{
		"Payload": base64encode(body),
		"Headers": map[string][]string{
			"content-type": {"application/json"},
		},
	}))
}

func handleExecutorExecuteStream(rawReq []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}

	modelID, _ := req["Model"].(string)
	if modelID == "" {
		return errorEnvelope("invalid_request", "missing model"), nil
	}
	if err := ensureModels(); err != nil {
		return errorEnvelope("model_refresh_failed", err.Error()), nil
	}

	baseModelID := resolveModel(modelID)
	if baseModelID != "" && !opencodeRefresher.Contains(baseModelID) {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}
	if !modelHealth.Allow(openCodeHealthScope(), baseModelID) {
		return errorEnvelope("model_quarantined", fmt.Sprintf("model %q is temporarily unavailable", modelID)), nil
	}

	openaiReq := shared.ConvertToOpenAI(req, baseModelID, resolveModel)
	openaiReq["stream"] = true
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	reader, status, err := executeOpenCodeChatStreamWithRetry(payload)
	if err != nil {
		recordInferenceFailure(openCodeHealthScope(), baseModelID, status, nil, err)
		return errorEnvelope("upstream_error", err.Error()), nil
	}
	if status != 200 {
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, io.LimitReader(reader, 1<<20))
		_ = reader.Close()
		body := buf.Bytes()
		recordInferenceFailure(openCodeHealthScope(), baseModelID, status, body, nil)
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+strconv.Itoa(status)+": "+buf.String(), status), nil
	}

	// Drain the SSE stream and encode each raw chunk for the host envelope.
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
			modelHealth.RecordProbeFailure(openCodeHealthScope(), baseModelID)
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
			modelHealth.RecordProbeFailure(openCodeHealthScope(), baseModelID)
			return errorEnvelope("executor_stream_failed", "stream exceeded max byte limit"), nil
		}
		chunks = append(chunks, map[string]any{"Payload": base64encode([]byte(line + "\n"))})
	}
	if err := scanner.Err(); err != nil {
		_ = reader.Close()
		modelHealth.RecordProbeFailure(openCodeHealthScope(), baseModelID)
		return errorEnvelope("executor_stream_failed", "stream read error: "+err.Error()), nil
	}
	_ = reader.Close()
	if len(chunks) == 0 {
		modelHealth.RecordProbeFailure(openCodeHealthScope(), baseModelID)
		return errorEnvelope("executor_stream_failed", "empty chat stream"), nil
	}
	modelHealth.RecordSuccess(openCodeHealthScope(), baseModelID)
	return okEnvelopeJSON(shared.MustJSON(map[string]any{
		"Headers": map[string]any{
			"content-type": []string{"text/event-stream"},
		},
		"Chunks": chunks,
	}))
}

func handleExecutorHTTPRequest(rawReq []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}

	method, _ := req["method"].(string)
	if method == "" {
		method = http.MethodGet
	}
	urlStr, _ := req["url"].(string)
	if urlStr == "" {
		return errorEnvelope("invalid_request", "missing url"), nil
	}

	body, _ := req["body"].([]byte)
	if body == nil {
		if b, ok := req["body"].(string); ok {
			body = []byte(b)
		}
	}

	reqHTTP, err := http.NewRequest(method, urlStr, bytes.NewReader(body))
	if err != nil {
		return errorEnvelope("invalid_request", err.Error()), nil
	}

	if headers, ok := req["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				reqHTTP.Header.Set(k, vs)
			} else if vv, ok := v.([]interface{}); ok {
				for _, vv2 := range vv {
					if vs, ok := vv2.(string); ok {
						reqHTTP.Header.Add(k, vs)
					}
				}
			}
		}
	}

	resp, err := httpClient.Do(reqHTTP)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}

	return okEnvelopeJSON(shared.MustJSON(map[string]interface{}{
		"StatusCode": resp.StatusCode,
		"Headers":    shared.HeaderMap(resp.Header),
		"Body":       base64encode(respBody),
	}))
}

func handleExecutorCountTokens(rawReq []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}

	modelID, _ := req["Model"].(string)
	if modelID == "" {
		return errorEnvelope("invalid_request", "missing model"), nil
	}

	// Estimate tokens: ~4 chars per token
	prompt, _ := req["prompt"].(string)
	if prompt == "" {
		if b, ok := req["prompt"].([]byte); ok {
			prompt = string(b)
		}
	}
	n := len(prompt) / 4
	if n == 0 && len(prompt) > 0 {
		n = 1
	}

	return okEnvelopeJSON(shared.MustJSON(map[string]interface{}{
		"Count": n,
	}))
}

// executeOpenCodeChat sends a chat completion request to OpenCode

func executeOpenCodeChat(payload []byte, stream bool) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, currentOpenCodeChatURL(), bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}

	// Set OpenCode headers
	headers := opencodeHeaders()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer public")
	req.Header.Set("Content-Type", "application/json")

	client := httpClient
	if stream {
		client = &http.Client{Transport: streamTransport}
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}

	return resp.StatusCode, body, nil
}

func executeOpenCodeChatStream(payload []byte) (io.ReadCloser, int, error) {
	req, err := http.NewRequest(http.MethodPost, currentOpenCodeChatURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}

	// Set OpenCode headers
	headers := opencodeHeaders()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer public")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: streamTransport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.StatusCode, nil
}

// executeOpenCodeChatWithRetry retries on transient 502/503 errors.
func executeOpenCodeChatWithRetry(payload []byte, stream bool) (int, []byte, error) {
	const maxAttempts = 3
	backoff := time.Second
	var status int
	var body []byte
	var err error
	for attempt := range maxAttempts {
		status, body, err = executeOpenCodeChat(payload, stream)
		if err != nil || status < 500 || status > 503 {
			return status, body, err
		}
		if attempt+1 < maxAttempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return status, body, nil
}

// executeOpenCodeChatStreamWithRetry retries stream on transient 502/503 errors.
func executeOpenCodeChatStreamWithRetry(payload []byte) (io.ReadCloser, int, error) {
	const maxAttempts = 3
	backoff := time.Second
	var reader io.ReadCloser
	var status int
	var err error
	for attempt := range maxAttempts {
		reader, status, err = executeOpenCodeChatStream(payload)
		if err != nil || status < 500 || status > 503 {
			return reader, status, err
		}
		_ = reader.Close()
		if attempt+1 < maxAttempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return reader, status, nil
}
