package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func handleExecutorIdentifier() ([]byte, error) {
	return okEnvelopeJSON(`{"Identifier":"` + EXECUTOR_ID + `"}`)
}

func handleExecutorExecute(rawReq []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}

	modelID, _ := req["model"].(string)
	if modelID == "" {
		return errorEnvelope("invalid_request", "missing model"), nil
	}

	if !modelIDs[modelID] {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	status, body, err := executeKiloChat(payload, false)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}

	return okEnvelopeJSON(mustJSON(map[string]interface{}{
		"StatusCode": status,
		"Headers": map[string]interface{}{
			"content-type": []string{"application/json"},
		},
		"Body": base64encode(body),
	}))
}

func handleExecutorExecuteStream(rawReq []byte) ([]byte, error) {
	var req map[string]interface{}
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return errorEnvelope("invalid_request", "bad json"), nil
	}

	modelID, _ := req["model"].(string)
	if modelID == "" {
		return errorEnvelope("invalid_request", "missing model"), nil
	}

	if !modelIDs[modelID] {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	status, body, err := executeKiloChat(payload, true)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}

	// Split SSE into chunks
	chunks := splitSSE(body)
	chunkResults := make([]map[string]interface{}, 0, len(chunks))
	for _, chunk := range chunks {
		chunkResults = append(chunkResults, map[string]interface{}{
			"Data": base64encode(chunk),
		})
	}

	return okEnvelopeJSON(mustJSON(map[string]interface{}{
		"StatusCode": status,
		"Headers": map[string]interface{}{
			"content-type": []string{"text/event-stream"},
		},
		"Chunks": chunkResults,
		"Done":   true,
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

	modelID, _ := req["model"].(string)
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

// executeKiloChat sends a chat completion request to KiloCode gateway
func executeKiloChat(payload []byte, stream bool) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, KILO_CHAT_URL, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}

	// Set KiloCode headers
	headers := kiloHeaders()
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := httpClient
	if stream {
		client = &http.Client{Timeout: 0} // No timeout for streaming
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

// splitSSE splits an SSE response into individual event chunks
func splitSSE(data []byte) [][]byte {
	var chunks [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var currentChunk bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			eventData := strings.TrimPrefix(line, "data: ")
			eventData = strings.TrimSpace(eventData)
			if eventData == "[DONE]" {
				if currentChunk.Len() > 0 {
					chunks = append(chunks, currentChunk.Bytes())
					currentChunk.Reset()
				}
				continue
			}
			if eventData != "" {
				currentChunk.WriteString(eventData)
				currentChunk.WriteByte('\n')
			}
		} else if line == "" && currentChunk.Len() > 0 {
			chunks = append(chunks, currentChunk.Bytes())
			currentChunk.Reset()
		}
	}
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.Bytes())
	}

	if len(chunks) == 0 {
		return [][]byte{data}
	}
	return chunks
}
