package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

type sseChunkChoice struct {
	Index        int    `json:"index"`
	FinishReason string `json:"finish_reason,omitempty"`
	Delta        struct {
		Role             string        `json:"role,omitempty"`
		Content          string        `json:"content,omitempty"`
		ReasoningContent string        `json:"reasoning_content,omitempty"`
		ToolCalls        []sseToolCall `json:"tool_calls,omitempty"`
	} `json:"delta"`
}

type sseToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type sseChunk struct {
	ID      string           `json:"id,omitempty"`
	Object  string           `json:"object,omitempty"`
	Created int64            `json:"created,omitempty"`
	Model   string           `json:"model,omitempty"`
	Choices []sseChunkChoice `json:"choices,omitempty"`
	Usage   *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// assembleChatCompletionFromSSE reads an SSE event stream from reader and reconstructs
// a standard non-streaming OpenAI chat.completion JSON response.
func assembleChatCompletionFromSSE(reader io.Reader, defaultModel string) ([]byte, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		chatID           string
		model            = defaultModel
		created          int64
		finishReason     = "stop"
		contentBuilder   strings.Builder
		reasoningBuilder strings.Builder
		toolCallsMap     = make(map[int]*sseToolCall)
		promptTokens     int
		completionTokens int
		totalTokens      int
	)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == ':' {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.ID != "" {
			chatID = chunk.ID
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Created != 0 {
			created = chunk.Created
		}
		if chunk.Usage != nil {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
			totalTokens = chunk.Usage.TotalTokens
		}

		for _, ch := range chunk.Choices {
			if ch.FinishReason != "" {
				finishReason = ch.FinishReason
			}
			if ch.Delta.Content != "" {
				contentBuilder.WriteString(ch.Delta.Content)
			}
			if ch.Delta.ReasoningContent != "" {
				reasoningBuilder.WriteString(ch.Delta.ReasoningContent)
			}
			for _, tc := range ch.Delta.ToolCalls {
				existing, exists := toolCallsMap[tc.Index]
				if !exists {
					existing = &sseToolCall{
						Index: tc.Index,
						ID:    tc.ID,
						Type:  tc.Type,
					}
					existing.Function.Name = tc.Function.Name
					toolCallsMap[tc.Index] = existing
				}
				if tc.ID != "" && existing.ID == "" {
					existing.ID = tc.ID
				}
				if tc.Type != "" && existing.Type == "" {
					existing.Type = tc.Type
				}
				if tc.Function.Name != "" && existing.Function.Name == "" {
					existing.Function.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					existing.Function.Arguments += tc.Function.Arguments
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if chatID == "" {
		chatID = "chatcmpl-" + shared.RandomHex(16)
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	if totalTokens == 0 && (promptTokens > 0 || completionTokens > 0) {
		totalTokens = promptTokens + completionTokens
	}

	message := map[string]any{
		"role": "assistant",
	}

	if len(toolCallsMap) > 0 {
		indices := make([]int, 0, len(toolCallsMap))
		for idx := range toolCallsMap {
			indices = append(indices, idx)
		}
		sort.Ints(indices)

		toolCalls := make([]any, 0, len(indices))
		for _, idx := range indices {
			tc := toolCallsMap[idx]
			toolCalls = append(toolCalls, map[string]any{
				"index": tc.Index,
				"id":    tc.ID,
				"type":  tc.Type,
				"function": map[string]any{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				},
			})
		}
		message["tool_calls"] = toolCalls
		if contentBuilder.Len() > 0 {
			message["content"] = contentBuilder.String()
		} else {
			message["content"] = nil
		}
		if finishReason == "stop" || finishReason == "" {
			finishReason = "tool_calls"
		}
	} else {
		message["content"] = contentBuilder.String()
	}

	if reasoningBuilder.Len() > 0 {
		message["reasoning_content"] = reasoningBuilder.String()
	}

	response := map[string]any{
		"id":      chatID,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}

	return json.Marshal(response)
}

type responseStreamCloser struct {
	io.Reader
	upstream io.Closer
	pipeRead *io.PipeReader
}

func (r *responseStreamCloser) Close() error {
	_ = r.pipeRead.Close()
	if r.upstream != nil {
		return r.upstream.Close()
	}
	return nil
}

// convertResponsesSSEToOpenAISSE translates an upstream OpenAI Responses API SSE stream
// (/zen/v1/responses) into a standard OpenAI chat.completion.chunk SSE stream.
func convertResponsesSSEToOpenAISSE(reader io.ReadCloser, defaultModel string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		chatID := "chatcmpl-" + shared.RandomHex(16)
		created := time.Now().Unix()
		model := defaultModel

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var raw map[string]any
			if err := json.Unmarshal([]byte(data), &raw); err != nil {
				continue
			}
			t, _ := raw["type"].(string)
			switch t {
			case "response.created":
				if resp, ok := raw["response"].(map[string]any); ok {
					if id, ok := resp["id"].(string); ok && id != "" {
						chatID = "chatcmpl-" + id
					}
					if m, ok := resp["model"].(string); ok && m != "" {
						model = m
					}
					if ca, ok := resp["created_at"].(float64); ok && ca > 0 {
						created = int64(ca)
					}
				}
			case "response.output_text.delta":
				delta, _ := raw["delta"].(string)
				chunk := map[string]any{
					"id":      chatID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   model,
					"choices": []any{
						map[string]any{
							"index": 0,
							"delta": map[string]any{
								"content": delta,
							},
						},
					},
				}
				b, _ := json.Marshal(chunk)
				_, _ = fmt.Fprintf(pw, "data: %s\n\n", b)
			case "response.completed", "response.incomplete":
				finishReason := "stop"
				if t == "response.incomplete" {
					finishReason = "length"
				}
				finalChunk := map[string]any{
					"id":      chatID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   model,
					"choices": []any{
						map[string]any{
							"index":         0,
							"delta":         map[string]any{},
							"finish_reason": finishReason,
						},
					},
				}
				if resp, ok := raw["response"].(map[string]any); ok {
					if u, ok := resp["usage"].(map[string]any); ok {
						finalChunk["usage"] = map[string]any{
							"prompt_tokens":     u["input_tokens"],
							"completion_tokens": u["output_tokens"],
							"total_tokens":      u["total_tokens"],
						}
					}
				}
				b, _ := json.Marshal(finalChunk)
				_, _ = fmt.Fprintf(pw, "data: %s\n\n", b)
				_, _ = fmt.Fprintf(pw, "data: [DONE]\n\n")
			}
		}
	}()
	return &responseStreamCloser{
		Reader:   pr,
		upstream: reader,
		pipeRead: pr,
	}
}
