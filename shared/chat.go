package shared

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

var streamTransport = &http.Transport{
	ResponseHeaderTimeout: 30 * time.Second,
}

// InjectNousPortalTags ensures the upstream Nous Portal request carries the
// expected Hermes product-attribution tags. If the host already supplied tags,
// we leave them alone.
func InjectNousPortalTags(payload []byte) []byte {
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

// DoChatRequest performs a non-streaming OpenAI chat-completions call.
func DoChatRequest(url, apiKey string, payload []byte) ([]byte, int, http.Header, error) {
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

// DoChatStream performs a streaming OpenAI chat-completions call.
func DoChatStream(url, apiKey string, payload []byte) (io.ReadCloser, int, http.Header, error) {
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

// CopyHeaders copies all values from src into dst.
func CopyHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// HeaderMap converts an http.Header into a map, dropping empty and
// sensitive headers (authorization/cookie/set-cookie).
func HeaderMap(h http.Header) map[string][]string {
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

// ConvertToOpenAI rebuilds an OpenAI-style request from the CPA envelope,
// stripping the provider prefix from the model id via strip. Identical logic
// previously duplicated in kilo-free and opencode-free.
func ConvertToOpenAI(req map[string]interface{}, modelID string, strip func(string) string) map[string]interface{} {
	if payload, ok := req["Payload"].(string); ok && payload != "" {
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err == nil {
			var openaiReq map[string]interface{}
			if err := json.Unmarshal(decoded, &openaiReq); err == nil {
				if m, ok := openaiReq["model"].(string); ok {
					openaiReq["model"] = strip(m)
				} else {
					openaiReq["model"] = modelID
				}
				return openaiReq
			}
		}
	}

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
