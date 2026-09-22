package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

const opencodeIDChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var (
	opencodeIDMu       sync.Mutex
	opencodeLastMillis int64
	opencodeCounter    int64
)

// generateOpenCodeID replicates OpenCode internal generator tU(descending, now).
// It produces a 6-byte (12 hex char) time+counter prefix, followed by 14 random alphanumeric chars.
func generateOpenCodeID(descending bool, nowMillis int64) string {
	opencodeIDMu.Lock()
	if nowMillis == 0 {
		nowMillis = time.Now().UnixMilli()
	}
	if nowMillis != opencodeLastMillis {
		opencodeLastMillis = nowMillis
		opencodeCounter = 0
	}
	opencodeCounter++
	counter := opencodeCounter
	opencodeIDMu.Unlock()

	val := (nowMillis * 0x1000) + counter
	if descending {
		val = ^val
	}

	// 6 bytes (48 bits) big-endian hex
	hexPrefix := fmt.Sprintf("%02x%02x%02x%02x%02x%02x",
		byte(val>>40),
		byte(val>>32),
		byte(val>>24),
		byte(val>>16),
		byte(val>>8),
		byte(val),
	)

	b := make([]byte, 14)
	if _, err := rand.Read(b); err != nil {
		return hexPrefix + shared.RandomHex(7)
	}

	suffix := make([]byte, 14)
	for i, v := range b {
		suffix[i] = opencodeIDChars[int(v)%len(opencodeIDChars)]
	}

	return hexPrefix + string(suffix)
}

func newOpenCodeSessionID() string {
	return "ses_" + generateOpenCodeID(true, 0)
}

func newOpenCodeRequestID() string {
	return "msg_" + generateOpenCodeID(false, 0)
}

func newOpenCodeProjectID() string {
	return shared.RandomHex(20)
}

// minimalOpenCodeTools returns the tools required to pass the OpenCode gate validation.
func minimalOpenCodeTools() []map[string]any {
	return []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":       "bash",
				"parameters": map[string]any{"type": "object"},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":       "read",
				"parameters": map[string]any{"type": "object"},
			},
		},
	}
}

// frameOpenCodePayload ensures that the payload satisfies OpenCode Zen upstream gate rules:
// 1. tools must include at least bash and read definitions (upstream FreeTier validation requirement).
// 2. If no tools were originally provided by user, tool_choice is set to "none" so model responds normally.
// 3. stream is enabled (as required by upstream free tier gate).
// 4. stream_options.include_usage is requested.
func frameOpenCodePayload(openaiReq map[string]any) map[string]any {
	reqCopy := make(map[string]any, len(openaiReq)+4)
	for k, v := range openaiReq {
		reqCopy[k] = v
	}

	var hasBash, hasRead bool
	userTools, hasTools := reqCopy["tools"].([]any)
	if hasTools {
		for _, t := range userTools {
			if tm, ok := t.(map[string]any); ok {
				if fn, ok := tm["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						if name == "bash" {
							hasBash = true
						} else if name == "read" {
							hasRead = true
						}
					}
				}
			}
		}
	}

	var toolsList []any
	if hasTools {
		toolsList = make([]any, len(userTools), len(userTools)+2)
		copy(toolsList, userTools)
	} else {
		toolsList = make([]any, 0, 2)
	}

	for _, reqTool := range minimalOpenCodeTools() {
		name := reqTool["function"].(map[string]any)["name"].(string)
		if (name == "bash" && !hasBash) || (name == "read" && !hasRead) {
			toolsList = append(toolsList, reqTool)
		}
	}
	reqCopy["tools"] = toolsList

	if !hasTools || len(userTools) == 0 {
		if _, hasChoice := reqCopy["tool_choice"]; !hasChoice {
			reqCopy["tool_choice"] = "none"
		}
	}

	reqCopy["stream"] = true
	reqCopy["stream_options"] = map[string]any{
		"include_usage": true,
	}

	return reqCopy
}

// isOpenCodeResponsesModel returns true for models requiring OpenAI Responses API (/zen/v1/responses).
func isOpenCodeResponsesModel(modelID string) bool {
	return strings.HasPrefix(modelID, "muse-spark")
}

// minimalOpenCodeResponsesTools returns flat function definitions required by Responses API.
func minimalOpenCodeResponsesTools() []map[string]any {
	return []map[string]any{
		{
			"type":        "function",
			"name":        "bash",
			"description": "run bash",
			"parameters":  map[string]any{"type": "object"},
		},
		{
			"type":        "function",
			"name":        "read",
			"description": "read file",
			"parameters":  map[string]any{"type": "object"},
		},
	}
}

// frameOpenCodeResponsesPayload formats the request for upstream /zen/v1/responses.
func frameOpenCodeResponsesPayload(openaiReq map[string]any) map[string]any {
	reqCopy := make(map[string]any, len(openaiReq)+4)
	for k, v := range openaiReq {
		reqCopy[k] = v
	}

	if _, hasInput := reqCopy["input"]; !hasInput {
		if msgs, ok := reqCopy["messages"]; ok {
			reqCopy["input"] = msgs
		}
	}
	delete(reqCopy, "messages")

	if maxTokens, ok := reqCopy["max_tokens"]; ok {
		reqCopy["max_output_tokens"] = maxTokens
		delete(reqCopy, "max_tokens")
	} else if maxTokens, ok := reqCopy["max_completion_tokens"]; ok {
		reqCopy["max_output_tokens"] = maxTokens
		delete(reqCopy, "max_completion_tokens")
	}

	if re, ok := reqCopy["reasoning_effort"].(string); ok && re != "" {
		reqCopy["reasoning"] = map[string]any{"effort": re}
		delete(reqCopy, "reasoning_effort")
	} else if _, hasReasoning := reqCopy["reasoning"]; !hasReasoning {
		reqCopy["reasoning"] = map[string]any{"effort": "minimal"}
	}

	var hasBash, hasRead bool
	userTools, hasTools := reqCopy["tools"].([]any)
	toolsList := make([]any, 0, len(userTools)+2)
	if hasTools {
		for _, t := range userTools {
			if tm, ok := t.(map[string]any); ok {
				name, _ := tm["name"].(string)
				desc, _ := tm["description"].(string)
				params := tm["parameters"]
				if fn, ok := tm["function"].(map[string]any); ok {
					if fnName, ok := fn["name"].(string); ok {
						name = fnName
					}
					if fnDesc, ok := fn["description"].(string); ok {
						desc = fnDesc
					}
					if fnParams, ok := fn["parameters"]; ok {
						params = fnParams
					}
				}
				if name == "bash" {
					hasBash = true
				} else if name == "read" {
					hasRead = true
				}
				flatTool := map[string]any{
					"type": "function",
					"name": name,
				}
				if desc != "" {
					flatTool["description"] = desc
				}
				if params != nil {
					flatTool["parameters"] = params
				} else {
					flatTool["parameters"] = map[string]any{"type": "object"}
				}
				toolsList = append(toolsList, flatTool)
			}
		}
	}

	for _, reqTool := range minimalOpenCodeResponsesTools() {
		name := reqTool["name"].(string)
		if (name == "bash" && !hasBash) || (name == "read" && !hasRead) {
			toolsList = append(toolsList, reqTool)
		}
	}
	reqCopy["tools"] = toolsList
	reqCopy["tool_choice"] = "auto"
	reqCopy["stream"] = true

	return reqCopy
}

// prepareOpenCodePayload unmarshals raw JSON payload, ensures proper framing
// (minimal tools, tool_choice, stream=true), and re-marshals.
func prepareOpenCodePayload(payload []byte, baseModelID string) []byte {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return payload
	}
	if baseModelID == "" {
		if mod, ok := m["model"].(string); ok {
			baseModelID = resolveModel(mod)
		}
	}
	var framed map[string]any
	if isOpenCodeResponsesModel(baseModelID) {
		framed = frameOpenCodeResponsesPayload(m)
	} else {
		framed = frameOpenCodePayload(m)
	}
	b, err := json.Marshal(framed)
	if err != nil {
		return payload
	}
	return b
}
