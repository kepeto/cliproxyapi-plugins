package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
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
		kiloRecordProbeFailure(scope, model)
		return
	}
	if shared.IsModelSpecificFailure(status, body, nil) {
		kiloRecordFailure(scope, model)
	}
}

var streamTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   20,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ResponseHeaderTimeout: 180 * time.Second,
	TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12,
	},
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
	if baseModelID != "" && !kiloRefresher.Contains(baseModelID) {
		if err := kiloRefresher.Refresh(); err != nil || !kiloRefresher.Contains(baseModelID) {
			return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
		}
	}
	if !kiloModelAllowed(kiloHealthScope(), baseModelID) {
		return errorEnvelope("model_quarantined", fmt.Sprintf("model %q is temporarily unavailable", modelID)), nil
	}

	openaiReq := shared.ConvertToOpenAI(req, baseModelID, resolveModel)
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	status, body, err := executeKiloChatWithRetry(payload)
	if err != nil {
		recordInferenceFailure(kiloHealthScope(), baseModelID, status, body, err)
		return errorEnvelope("upstream_error", err.Error()), nil
	}

	if status < 200 || status >= 300 {
		recordInferenceFailure(kiloHealthScope(), baseModelID, status, body, nil)
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+strconv.Itoa(status)+": "+string(body), status), nil
	}
	if !shared.ValidChatResponse(body) {
		kiloRecordProbeFailure(kiloHealthScope(), baseModelID)
		return errorEnvelope("upstream_error", "invalid or empty chat response"), nil
	}
	kiloRecordSuccess(kiloHealthScope(), baseModelID)
	return okEnvelopeJSON(shared.MustJSON(map[string]interface{}{
		"Payload": base64encode(body),
		"Headers": map[string][]string{"content-type": {"application/json"}},
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
	if baseModelID != "" && !kiloRefresher.Contains(baseModelID) {
		if err := kiloRefresher.Refresh(); err != nil || !kiloRefresher.Contains(baseModelID) {
			return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
		}
	}
	if !kiloModelAllowed(kiloHealthScope(), baseModelID) {
		return errorEnvelope("model_quarantined", fmt.Sprintf("model %q is temporarily unavailable", modelID)), nil
	}

	openaiReq := shared.ConvertToOpenAI(req, baseModelID, resolveModel)
	openaiReq["stream"] = true
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	reader, status, err := executeKiloChatStreamWithRetry(payload)
	if err != nil {
		recordInferenceFailure(kiloHealthScope(), baseModelID, status, nil, err)
		return errorEnvelope("upstream_error", err.Error()), nil
	}
	if status != 200 {
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, io.LimitReader(reader, 1<<20))
		_ = reader.Close()
		body := buf.Bytes()
		recordInferenceFailure(kiloHealthScope(), baseModelID, status, body, nil)
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+strconv.Itoa(status)+": "+buf.String(), status), nil
	}

	// Drain the SSE stream and encode each raw chunk as base64 for the host envelope.
	const (
		maxStreamChunks = 100000
		maxStreamBytes  = 100 * 1024 * 1024
	)
	chunks := make([]map[string]any, 0)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var totalBytes int
	var sawEvent, sawDone bool
	for scanner.Scan() {
		if len(chunks) >= maxStreamChunks {
			_ = reader.Close()
			kiloRecordProbeFailure(kiloHealthScope(), baseModelID)
			return errorEnvelope("executor_stream_failed", "stream exceeded max chunk limit"), nil
		}
		line := scanner.Text()
		if line == "" || line[0] == ':' {
			continue
		}
		trimmed := strings.TrimPrefix(line, "data:")
		trimmed = strings.TrimPrefix(trimmed, " ")
		totalBytes += len(trimmed) + 1
		if totalBytes > maxStreamBytes {
			_ = reader.Close()
			kiloRecordProbeFailure(kiloHealthScope(), baseModelID)
			return errorEnvelope("executor_stream_failed", "stream exceeded max byte limit"), nil
		}
		switch {
		case trimmed == "[DONE]":
			sawDone = true
		default:
			var event map[string]any
			if json.Unmarshal([]byte(trimmed), &event) == nil {
				if _, ok := event["choices"]; ok {
					sawEvent = true
				}
			}
		}
		chunks = append(chunks, map[string]any{"Payload": []byte(trimmed + "\n")})
	}
	if err := scanner.Err(); err != nil {
		_ = reader.Close()
		kiloRecordProbeFailure(kiloHealthScope(), baseModelID)
		return errorEnvelope("executor_stream_failed", "stream read error: "+err.Error()), nil
	}
	_ = reader.Close()
	if len(chunks) == 0 {
		kiloRecordProbeFailure(kiloHealthScope(), baseModelID)
		return errorEnvelope("executor_stream_failed", "empty chat stream"), nil
	}
	if !sawEvent || !sawDone {
		kiloRecordProbeFailure(kiloHealthScope(), baseModelID)
		return errorEnvelope("executor_stream_failed", "incomplete chat stream: missing completion event or [DONE]"), nil
	}
	kiloRecordSuccess(kiloHealthScope(), baseModelID)
	return okEnvelopeJSON(shared.MustJSON(map[string]any{
		"Headers": map[string]any{
			"content-type": []string{"text/event-stream"},
		},
		"Chunks": chunks,
	}))
}

func executeKiloChat(payload []byte, stream bool) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, currentKiloChatURL(), bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	headers := kiloHeaders()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Transport: streamTransport, Timeout: HTTP_TIMEOUT}
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

func executeKiloChatStream(payload []byte) (io.ReadCloser, int, error) {
	req, err := http.NewRequest(http.MethodPost, currentKiloChatURL(), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	headers := kiloHeaders()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Transport: streamTransport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.StatusCode, nil
}

func handleExecutorHTTPRequest(rawReq []byte) ([]byte, error) {
	var req map[string]any
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

	var body []byte
	switch value := req["body"].(type) {
	case string:
		body = []byte(value)
	case []byte:
		body = value
	}
	reqHTTP, err := http.NewRequest(method, urlStr, bytes.NewReader(body))
	if err != nil {
		return errorEnvelope("invalid_request", err.Error()), nil
	}
	if headers, ok := req["headers"].(map[string]any); ok {
		for key, value := range headers {
			switch values := value.(type) {
			case string:
				reqHTTP.Header.Set(key, values)
			case []any:
				for _, item := range values {
					if value, ok := item.(string); ok {
						reqHTTP.Header.Add(key, value)
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
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}
	return okEnvelopeJSON(shared.MustJSON(map[string]any{
		"StatusCode": resp.StatusCode,
		"Headers":    shared.HeaderMap(resp.Header),
		"Body":       base64encode(responseBody),
	}))
}

func handleExecutorCountTokens(rawReq []byte) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}
	modelID, _ := req["Model"].(string)
	if modelID == "" {
		return errorEnvelope("invalid_request", "missing model"), nil
	}

	prompt, _ := req["prompt"].(string)
	if prompt == "" {
		if payload, ok := req["Payload"].(string); ok {
			prompt = payload
		}
	}
	n := len(prompt) / 4
	if n == 0 && len(prompt) > 0 {
		n = 1
	}
	return okEnvelopeJSON(shared.MustJSON(map[string]any{"Count": n}))
}

func executeKiloChatStreamWithRetry(payload []byte) (io.ReadCloser, int, error) {
	const maxAttempts = 3
	backoff := time.Second
	var reader io.ReadCloser
	var status int
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		reader, status, err = executeKiloChatStream(payload)
		if err == nil && (status < http.StatusBadGateway || status > http.StatusGatewayTimeout) {
			return reader, status, nil
		}
		if reader != nil {
			_ = reader.Close()
		}
		if attempt+1 < maxAttempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return reader, status, err
}

func executeKiloChatWithRetry(payload []byte) (int, []byte, error) {
	const maxAttempts = 3
	backoff := time.Second
	var status int
	var body []byte
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		status, body, err = executeKiloChat(payload, false)
		if err == nil && (status < http.StatusBadGateway || status > http.StatusGatewayTimeout) {
			return status, body, nil
		}
		if attempt+1 < maxAttempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return status, body, err
}
