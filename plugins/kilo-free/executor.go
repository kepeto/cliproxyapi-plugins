package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

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

	func handleExecutorIdentifier() ([]byte, error) {
	return okEnvelopeJSON(`{"Identifier":"` + EXECUTOR_ID + `"}`)
}

func convertToOpenAI(req map[string]interface{}, modelID string) map[string]interface{} {
		// Prefer decoded Payload if present
		if payload, ok := req["Payload"].(string); ok && payload != "" {
			decoded, err := base64.StdEncoding.DecodeString(payload)
			if err == nil {
				var openaiReq map[string]interface{}
				if err := json.Unmarshal(decoded, &openaiReq); err == nil {
					if m, ok := openaiReq["model"].(string); ok {
						openaiReq["model"] = stripModelPrefix(m)
					} else {
						openaiReq["model"] = modelID
					}
					return openaiReq
				}
			}
		}
		
		// Fallback: reconstruct from envelope fields
		openaiReq := map[string]interface{}{
			"model":    modelID,
			"messages": []interface{}{},
		}

		if messages, ok := req["Messages"].([]interface{}); ok {
			openaiReq["messages"] = messages
		} else if messages, ok := req["Messages"].([]map[string]interface{}); ok {
			openaiReq["messages"] = messages
		}

		if temp, ok := req["Temperature"].(float64); ok {
			openaiReq["temperature"] = temp
		}
		if maxTokens, ok := req["MaxTokens"].(float64); ok {
			openaiReq["max_tokens"] = int(maxTokens)
		}
		if topP, ok := req["TopP"].(float64); ok {
			openaiReq["top_p"] = topP
		}
		if stop, ok := req["Stop"].([]interface{}); ok {
			openaiReq["stop"] = stop
		}
		if stream, ok := req["Stream"].(bool); ok {
			openaiReq["stream"] = stream
		}
		if seed, ok := req["Seed"].(float64); ok {
			openaiReq["seed"] = int(seed)
		}
		if freqPenalty, ok := req["FrequencyPenalty"].(float64); ok {
			openaiReq["frequency_penalty"] = freqPenalty
		}
		if presPenalty, ok := req["PresencePenalty"].(float64); ok {
			openaiReq["presence_penalty"] = presPenalty
		}

		return openaiReq
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

	if !modelIDs[modelID] {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}

	openaiReq := convertToOpenAI(req, modelID)
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	status, body, err := executeKiloChat(payload, false)
	// Debug
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}

	if status < 200 || status >= 300 {
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+strconv.Itoa(status)+": "+string(body), status), nil
	}
	return okEnvelopeJSON(mustJSON(map[string]interface{}{
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
	baseModelID := stripModelPrefix(modelID)
	if !modelIDs[baseModelID] {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}

	openaiReq := convertToOpenAI(req, baseModelID)
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	reader, status, err := executeKiloChatStream(payload)
	if err != nil {
		return errorEnvelope("upstream_error", err.Error()), nil
	}
	if status != 200 {
		buf := new(bytes.Buffer)
		_, _ = io.Copy(buf, reader)
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

func executeKiloChat(payload []byte, stream bool) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, KILO_CHAT_URL, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	headers := kiloHeaders()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := httpClient
	if stream {
		client = &http.Client{Timeout: 0}
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

func executeKiloChatStream(payload []byte) (io.ReadCloser, int, error) {
		req, err := http.NewRequest(http.MethodPost, KILO_CHAT_URL, bytes.NewReader(payload))
		if err != nil {
			return nil, 0, err
		}
		headers := kiloHeaders()
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		client := &http.Client{Timeout: 0} // No timeout for streaming
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		return resp.Body, resp.StatusCode, nil
	}

	func handleExecutorHTTPRequest(rawReq []byte) ([]byte, error) {
	return errorEnvelope("not_implemented", "http_request not supported"), nil
}

func handleExecutorCountTokens(rawReq []byte) ([]byte, error) {
	return errorEnvelope("not_implemented", "count_tokens not supported"), nil
}
