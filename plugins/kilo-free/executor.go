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
	if baseModelID != "" && !kiloRefresher.Contains(baseModelID) {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}

	openaiReq := shared.ConvertToOpenAI(req, baseModelID, resolveModel)
	payload, err := json.Marshal(openaiReq)
	if err != nil {
		return errorEnvelope("invalid_request", "marshal error"), nil
	}

	status, body, err := executeKiloChat(payload, false)
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
	baseModelID := resolveModel(modelID)
	if baseModelID != "" && !kiloRefresher.Contains(baseModelID) {
		return errorEnvelope("model_not_found", fmt.Sprintf("model %q not found", modelID)), nil
	}

	openaiReq := shared.ConvertToOpenAI(req, baseModelID, resolveModel)
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
		_, _ = io.Copy(buf, io.LimitReader(reader, 1<<20))
		_ = reader.Close()
		return errorEnvelopeWithStatus("upstream_error", "inference returned "+itoa(status)+": "+buf.String(), status), nil
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
		"Headers": map[string]any{
			"content-type": []string{"text/event-stream"},
		},
		"Chunks": chunks,
	}))
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
	client := &http.Client{Transport: streamTransport}
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
