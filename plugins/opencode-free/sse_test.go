package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestAssembleChatCompletionFromSSE(t *testing.T) {
	sseInput := `
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{"content":" world!"}}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.6-flash-free","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`

	respBytes, err := assembleChatCompletionFromSSE(strings.NewReader(sseInput), "default-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if resp["id"] != "chatcmpl-123" {
		t.Errorf("expected id chatcmpl-123, got %v", resp["id"])
	}
	if resp["model"] != "mimo-v2.6-flash-free" {
		t.Errorf("expected model mimo-v2.6-flash-free, got %v", resp["model"])
	}

	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("missing choices")
	}
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	if msg["content"] != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", msg["content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason 'stop', got %v", choice["finish_reason"])
	}

	usage := resp["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 10 || usage["completion_tokens"].(float64) != 5 || usage["total_tokens"].(float64) != 15 {
		t.Errorf("unexpected usage: %v", usage)
	}
}

func TestAssembleChatCompletionFromSSEToolCalls(t *testing.T) {
	sseInput := `
data: {"id":"chatcmpl-tc","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"bash","arguments":""}}]}}]}

data: {"id":"chatcmpl-tc","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":"}}]}}]}

data: {"id":"chatcmpl-tc","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]}}]}

data: {"id":"chatcmpl-tc","object":"chat.completion.chunk","created":1700000000,"model":"mimo-v2.6-flash-free","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`

	respBytes, err := assembleChatCompletionFromSSE(strings.NewReader(sseInput), "default-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	choices := resp["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got %v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	toolCalls := msg["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	tc := toolCalls[0].(map[string]any)
	if tc["id"] != "call_abc" {
		t.Errorf("expected tool call id call_abc, got %v", tc["id"])
	}
	fn := tc["function"].(map[string]any)
	if fn["name"] != "bash" {
		t.Errorf("expected function name bash, got %v", fn["name"])
	}
	if fn["arguments"] != `{"cmd":"ls"}` {
		t.Errorf("expected arguments '{\"cmd\":\"ls\"}', got %v", fn["arguments"])
	}
}

func TestConvertResponsesSSEToOpenAISSE(t *testing.T) {
	responsesSSE := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_999","created_at":1700000000,"model":"muse-spark-1.3-contributor-free"}}`,
		`data: {"type":"response.output_text.delta","delta":"Hello "}`,
		`data: {"type":"response.output_text.delta","delta":"world!"}`,
		`data: {"type":"response.completed","response":{"id":"resp_999","created_at":1700000000,"model":"muse-spark-1.3-contributor-free","usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}`,
		"",
	}, "\n\n")

	converted := convertResponsesSSEToOpenAISSE(io.NopCloser(strings.NewReader(responsesSSE)), "muse-spark-1.3-contributor-free")
	defer converted.Close()

	assembledBytes, err := assembleChatCompletionFromSSE(converted, "muse-spark-1.3-contributor-free")
	if err != nil {
		t.Fatalf("assembleChatCompletionFromSSE failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(assembledBytes, &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if resp["id"] != "chatcmpl-resp_999" {
		t.Errorf("expected id chatcmpl-resp_999, got %v", resp["id"])
	}
	choices := resp["choices"].([]any)
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	if msg["content"] != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", msg["content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason 'stop', got %v", choice["finish_reason"])
	}
	usage := resp["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 10 || usage["completion_tokens"].(float64) != 3 || usage["total_tokens"].(float64) != 13 {
		t.Errorf("unexpected usage: %v", usage)
	}
}
