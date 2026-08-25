package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int PlaygroundPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void PlaygroundPluginFree(void*, size_t);
extern void PlaygroundPluginShutdown(void);

static const cliproxy_host_api* playground_host;

static void playground_store_host(const cliproxy_host_api* host) {
	playground_host = host;
}

static int playground_call_host(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (playground_host == NULL || playground_host->call == NULL) {
		return 1;
	}
	return playground_host->call(playground_host->host_ctx, method, request, request_len, response);
}

static void playground_free_host_buffer(void* ptr, size_t len) {
	if (playground_host != NULL && playground_host->free_buffer != NULL && ptr != NULL) {
		playground_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

const (
	pluginID      = "playground"
	pluginName    = "Playground"
	pluginVersion = "0.1.0"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if host == nil || plugin == nil {
		return 1
	}
	C.playground_store_host(host)
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.PlaygroundPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.PlaygroundPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.PlaygroundPluginShutdown)
	return 0
}

//export PlaygroundPluginCall
func PlaygroundPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	raw, err := handleMethod(C.GoString(method), request, requestLen)
	if err != nil {
		writeResponse(response, errorEnvelope("plugin_error", err.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export PlaygroundPluginFree
func PlaygroundPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export PlaygroundPluginShutdown
func PlaygroundPluginShutdown() {
	C.playground_store_host(nil)
}

func handleMethod(method string, request *C.uint8_t, requestLen C.size_t) ([]byte, error) {
	switch method {
	case "plugin.register":
		return okEnvelopeJSON(registerPayload())
	case "plugin.reconfigure":
		return okEnvelopeJSON(registerPayload())
	case "management.register":
		return okEnvelopeJSON(managementRegisterPayload())
	case "management.handle":
		resp := handleManagementRequest(request, requestLen)
		respJSON, _ := json.Marshal(resp)
		return okEnvelopeJSON(string(respJSON))
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func registerPayload() string {
	return fmt.Sprintf(`{
		"schema_version": 1,
		"metadata": {
			"Name": "%s",
			"Version": "%s",
			"Author": "kepeto",
			"GitHubRepository": "https://github.com/kepeto/cliproxyapi-plugins",
			"Logo": "https://raw.githubusercontent.com/kepeto/cliproxyapi-plugins/main/plugins/playground/ui/logo.png",
			"ConfigFields": [
				{"name": "title", "type": "string", "default": "Playground", "description": "Page title"}
			]
		},
		"capabilities": {
			"management_api": true
		}
	}`, pluginName, pluginVersion)
}

func managementRegisterPayload() string {
	resources := []map[string]string{
		{"Path": "/config", "Menu": "Playground", "Description": "LLM Playground chat interface"},
		{"Path": "/app.js", "Menu": "", "Description": "Playground JavaScript"},
		{"Path": "/styles.css", "Menu": "", "Description": "Playground styles"},
	}
	routes := []map[string]string{
		{"Method": "POST", "Path": "/plugins/" + pluginID + "/api/models", "Menu": "", "Description": "List available models"},
		{"Method": "POST", "Path": "/plugins/" + pluginID + "/api/auths", "Menu": "", "Description": "List available auths"},
		{"Method": "POST", "Path": "/plugins/" + pluginID + "/api/chat", "Menu": "", "Description": "Non-streaming chat completion"},
		{"Method": "POST", "Path": "/plugins/" + pluginID + "/api/chat/stream", "Menu": "", "Description": "Streaming chat completion"},
	}
	raw, _ := json.Marshal(map[string]any{
		"resources": resources,
		"routes":    routes,
	})
	return string(raw)
}

type managementResponse struct {
	StatusCode int                 `json:"status_code,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body,omitempty"`
}

func handleManagementRequest(request *C.uint8_t, requestLen C.size_t) managementResponse {
	var req struct {
		Method         string              `json:"Method"`
		Path           string              `json:"Path"`
		Query          string              `json:"Query"`
		Body           []byte              `json:"Body"`
		Headers        map[string][]string `json:"Headers"`
		HostCallbackID string              `json:"HostCallbackID"`
	}
	if requestLen > 0 {
		body := C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
		json.Unmarshal(body, &req)
	}

	managementKey := extractManagementKey(req.Headers)
	path := cleanManagementPath(req.Path)

	switch path {
	case "", "/", "/config":
		data, err := embeddedAsset("index.html")
		if err != nil {
			return managementResponse{StatusCode: 404, Body: encodeBase64([]byte(err.Error()))}
		}
		return managementResponse{StatusCode: 200, Headers: map[string][]string{"content-type": {"text/html; charset=utf-8"}}, Body: encodeBase64(data)}
	case "/app.js":
		data, err := embeddedAsset("app.js")
		if err != nil {
			return managementResponse{StatusCode: 404, Body: encodeBase64([]byte(err.Error()))}
		}
		return managementResponse{StatusCode: 200, Headers: map[string][]string{"content-type": {"application/javascript"}}, Body: encodeBase64(data)}
	case "/styles.css":
		data, err := embeddedAsset("styles.css")
		if err != nil {
			return managementResponse{StatusCode: 404, Body: encodeBase64([]byte(err.Error()))}
		}
		return managementResponse{StatusCode: 200, Headers: map[string][]string{"content-type": {"text/css; charset=utf-8"}}, Body: encodeBase64(data)}
	case "/api/models":
		resp := handleListModels(req.Body, managementKey)
		if resp.Body != "" && !isBase64(resp.Body) {
			resp.Body = encodeBase64([]byte(resp.Body))
		}
		return resp
	case "/api/auths":
		resp := handleListAuths(req.Body)
		if resp.Body != "" && !isBase64(resp.Body) {
			resp.Body = encodeBase64([]byte(resp.Body))
		}
		return resp
	case "/api/chat":
		resp := handleChat(req.Body, false, managementKey)
		if resp.Body != "" && !isBase64(resp.Body) {
			resp.Body = encodeBase64([]byte(resp.Body))
		}
		return resp
	case "/api/chat/stream":
		resp := handleChat(req.Body, true, managementKey)
		if resp.Body != "" && !isBase64(resp.Body) {
			resp.Body = encodeBase64([]byte(resp.Body))
		}
		return resp
	default:
		return managementResponse{StatusCode: 404, Body: encodeBase64([]byte(`{"error":"not found"}`))}
	}
}

func isBase64(s string) bool {
	if s == "" {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
}

func extractManagementKey(headers map[string][]string) string {
	if headers == nil {
		return ""
	}
	for key, values := range headers {
		if strings.EqualFold(strings.TrimSpace(key), "Authorization") {
			for _, v := range values {
				v = strings.TrimSpace(v)
				if strings.HasPrefix(strings.ToLower(v), "bearer ") {
					return strings.TrimSpace(v[7:])
				}
			}
		}
	}
	return ""
}

func cleanManagementPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// Strip both resource and management plugin prefixes.
	resourcePrefix := "/v0/resource/plugins/" + pluginID
	managementPrefix := "/v0/management/plugins/" + pluginID
	if strings.HasPrefix(path, resourcePrefix+"/") {
		return strings.TrimPrefix(path, resourcePrefix)
	}
	if path == resourcePrefix {
		return "/"
	}
	if strings.HasPrefix(path, managementPrefix+"/") {
		return strings.TrimPrefix(path, managementPrefix)
	}
	if path == managementPrefix {
		return "/"
	}
	return path
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func okEnvelopeJSON(result string) ([]byte, error) {
	return json.Marshal(envelope{OK: true, Result: json.RawMessage(result)})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{
		OK:    false,
		Error: &envelopeError{Code: code, Message: message},
	})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}

func callHost(method string, payload []byte) []byte {
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var response C.cliproxy_buffer
	var req *C.uint8_t
	if len(payload) > 0 {
		req = (*C.uint8_t)(C.CBytes(payload))
		defer C.free(unsafe.Pointer(req))
	}
	if C.playground_call_host(cMethod, req, C.size_t(len(payload)), &response) == 0 && response.ptr != nil {
		raw := C.GoBytes(response.ptr, C.int(response.len))
		C.playground_free_host_buffer(response.ptr, response.len)
		return raw
	}
	return nil
}

func unwrapHTTPResponse(raw []byte) managementResponse {
	var outer struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil || !outer.OK {
		return managementResponse{StatusCode: 500, Body: string(raw)}
	}
	var httpResp struct {
		StatusCode int                 `json:"StatusCode"`
		Headers    map[string][]string `json:"Headers"`
		Body       string              `json:"Body"`
	}
	if err := json.Unmarshal(outer.Result, &httpResp); err != nil {
		return managementResponse{StatusCode: 500, Body: string(outer.Result)}
	}
	// Body from host.http.do is base64-encoded []byte in JSON; decode it.
	decodedBody, _ := base64.StdEncoding.DecodeString(httpResp.Body)
	bodyStr := string(decodedBody)
	if httpResp.Headers != nil {
		cleanHeaders := make(map[string][]string, len(httpResp.Headers))
		for k, v := range httpResp.Headers {
			// Skip Content-Length because the decoded body length differs from
			// the base64-encoded payload length carried in the host response.
			if strings.EqualFold(k, "Content-Length") {
				continue
			}
			cleanHeaders[k] = v
		}
		return managementResponse{StatusCode: httpResp.StatusCode, Headers: cleanHeaders, Body: bodyStr}
	}
	return managementResponse{StatusCode: httpResp.StatusCode, Body: bodyStr}
}

func handleListModels(body []byte, managementKey string) managementResponse {
	reqBody, _ := json.Marshal(map[string]any{
		"method":  "GET",
		"url":     "http://127.0.0.1:8317/v1/models",
		"headers": map[string][]string{"Authorization": {"Bearer " + managementKey}},
	})
	raw := callHost("host.http.do", reqBody)
	if raw == nil {
		return managementResponse{StatusCode: 500, Body: `{"error":"host_error","message":"failed to list models"}`}
	}
	return unwrapHTTPResponse(raw)
}

func handleListAuths(body []byte) managementResponse {
	raw := callHost("host.auth.list", body)
	if raw == nil {
		return managementResponse{StatusCode: 500, Body: encodeBase64([]byte(`{"error":"host_error","message":"failed to list auths"}`))}
	}
	var outer struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &outer); err != nil || !outer.OK {
		return managementResponse{StatusCode: 500, Body: encodeBase64([]byte(string(raw)))}
	}
	var authResp struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(outer.Result, &authResp); err != nil {
		return managementResponse{StatusCode: 500, Body: encodeBase64([]byte(string(outer.Result)))}
	}
	authList := make([]map[string]any, 0, len(authResp.Files))
	for _, f := range authResp.Files {
		entry := map[string]any{
			"id":         f["id"],
			"auth_index": f["auth_index"],
			"name":       f["name"],
			"type":       f["type"],
			"provider":   f["provider"],
		}
		authList = append(authList, entry)
	}
	resultJSON, _ := json.Marshal(authList)
	return managementResponse{StatusCode: 200, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: string(resultJSON)}
}

func handleChat(body []byte, stream bool, managementKey string) managementResponse {
	method := "POST"
	url := "http://127.0.0.1:8317/v1/chat/completions"
	hostMethod := "host.http.do"
	if stream {
		hostMethod = "host.http.do_stream"
	}
	reqBody, _ := json.Marshal(map[string]any{
		"method":  method,
		"url":     url,
		"headers": map[string][]string{"Authorization": {"Bearer " + managementKey}, "Content-Type": {"application/json"}},
		"body":    body,
	})
	raw := callHost(hostMethod, reqBody)
	if raw == nil {
		return managementResponse{StatusCode: 500, Body: encodeBase64([]byte(`{"error":"host_error","message":"failed to execute chat"}`))}
	}
	return unwrapHTTPResponse(raw)
}
