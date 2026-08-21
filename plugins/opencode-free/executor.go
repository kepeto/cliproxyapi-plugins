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

	baseModelID := resolveModel(modelID)
	if baseModelID != "" && !opencodeRefresher.Contains(baseModelID) {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}

	openaiReq := shared.ConvertToOpenAI(req, baseModelID, resolveModel)
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	status, body, err := executeOpenCodeChatWithRetry(payload, false)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}

	if status < 200 || status >= 300 {
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+strconv.Itoa(status)+": "+string(body), status), nil
	}
	return okEnvelopeJSON(mustJSON(map[string]interface{}{
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

	baseModelID := resolveModel(modelID)
	if baseModelID != "" && !opencodeRefresher.Contains(baseModelID) {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}

	openaiReq := shared.ConvertToOpenAI(req, baseModelID, resolveModel)
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	reader, status, err := executeOpenCodeChatStreamWithRetry(payload)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}
	if status != 200 {
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, io.LimitReader(reader, 1<<20))
		_ = reader.Close()
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+itoa(status)+": "+buf.String(), status), nil
	}

	// Drain the SSE stream and encode each raw chunk as base64 for the host envelope.
	chunks := make([]map[string]any, 0)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == ':' {
			continue
		}
		line = strings.TrimPrefix(line, "data: ")
		chunks = append(chunks, map[string]any{"Payload": base64encode([]byte(line + "\n"))})
	}
	if err := scanner.Err(); err != nil {
		chunks = append(chunks, map[string]any{"Err": err.Error()})
	}
	_ = reader.Close()
	return okEnvelopeJSON(mustJSON(map[string]any{
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

	status, respBody, err := httpDo(reqHTTP)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}

	headers := make(map[string]interface{})
	for k, v := range reqHTTP.Header {
		headers[strings.ToLower(k)] = v
	}

	return okEnvelopeJSON(mustJSON(map[string]interface{}{
		"StatusCode": status,
		"Headers":    headers,
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

	return okEnvelopeJSON(mustJSON(map[string]interface{}{
		"Count": n,
	}))
}

// executeOpenCodeChat sends a chat completion request to OpenCode
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func executeOpenCodeChat(payload []byte, stream bool) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, OPENCODE_CHAT_URL, bytes.NewReader(payload))
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

	body, err := ioReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}

	return resp.StatusCode, body, nil
}

func executeOpenCodeChatStream(payload []byte) (io.ReadCloser, int, error) {
	req, err := http.NewRequest(http.MethodPost, OPENCODE_CHAT_URL, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}

	// Set OpenCode headers
	headers := opencodeHeaders()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
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
	const maxRetries = 3
	backoff := 1 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		status, body, err := executeOpenCodeChat(payload, stream)
		if err != nil {
			return status, body, err
		}
		if status < 500 || status > 503 {
			return status, body, nil
		}
		if attempt < maxRetries-1 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return executeOpenCodeChat(payload, stream)
}

// executeOpenCodeChatStreamWithRetry retries stream on transient 502/503 errors.
func executeOpenCodeChatStreamWithRetry(payload []byte) (io.ReadCloser, int, error) {
	const maxRetries = 3
	backoff := 1 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		reader, status, err := executeOpenCodeChatStream(payload)
		if err != nil {
			return nil, status, err
		}
		if status < 500 || status > 503 {
			return reader, status, nil
		}
		_ = reader.Close()
		if attempt < maxRetries-1 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return executeOpenCodeChatStream(payload)
}
