package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	uint32_t abi_version;
	void (*call)(void *call_ctx, const char *method, const uint8_t *payload, size_t payload_len, uint8_t **out, size_t *out_len);
	void (*free_buffer)(void *call_ctx, void *ptr, size_t len);
	void (*shutdown)(void);
} cliproxy_plugin_api;

typedef struct {
	void *call_ctx;
	int (*call)(void *call_ctx, const char *method, const uint8_t *payload, size_t payload_len, uint8_t **out, size_t *out_len);
	void (*free)(void *call_ctx, void *ptr, size_t len);
	void (*log)(void *call_ctx, const char *level, const char *message);
} cliproxy_host_api;

typedef struct {
	uint8_t *ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_plugin_init_fn)(const cliproxy_host_api *host, cliproxy_plugin_api *plugin);
typedef int (*cliproxy_plugin_call_fn)(const char *method, const uint8_t *payload, size_t payload_len, cliproxy_buffer *response);
typedef void (*cliproxy_plugin_free_fn)(void *ptr, size_t len);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
*/
import "C"

import (
	"encoding/json"
	"unsafe"
	
)

// abiVersion must match pluginabi.ABIVersion (1).
const abiVersion uint32 = 1

type envelope struct {
	OK    bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	m := C.GoString(method)
	rawReq := copyCBuffer(request, requestLen)

	rawResp, err := dispatch(m, rawReq)
	if err != nil {
		if de, ok := err.(*dispatchError); ok {
			rawResp = errorEnvelope(de.Code(), de.Message())
		} else {
			rawResp = errorEnvelope("dispatch_error", err.Error())
		}
	}

	writeResponse(response, rawResp)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	C.free(ptr)
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	// nothing to clean up currently
}

func copyCBuffer(ptr *C.uint8_t, length C.size_t) []byte {
	if ptr == nil || length == 0 {
		return nil
	}
	slice := C.GoBytes(unsafe.Pointer(ptr), C.int(length))
	return slice
}

func dispatch(method string, rawReq []byte) ([]byte, error) {
	switch method {
	case "plugin.register":
		return okEnvelopeJSON(registerPayload())
	case "plugin.reconfigure":
		return okEnvelopeJSON(registerPayload())

	case "auth.identifier":
		return okEnvelopeJSON(`{"Identifier":"` + PROVIDER_ID + `"}`)
	case "auth.parse":
		return handleAuthParse(rawReq)
	case "auth.login.start":
		return errorEnvelope("login_not_supported", "no login needed for free models"), nil
	case "auth.login.poll":
		return errorEnvelope("login_not_supported", "no login needed for free models"), nil
	case "auth.refresh":
		return errorEnvelope("refresh_not_supported", "no refresh needed for free models"), nil


case "model.static":
		return handleModelStatic(rawReq)

	case "model.for_auth":
		return handleModelForAuth(rawReq)

	case "executor.identifier":
		return handleExecutorIdentifier()
	case "executor.execute":
		return handleExecutorExecute(rawReq)
	case "executor.execute_stream":
		return handleExecutorExecuteStream(rawReq)
	case "executor.http_request":
		return handleExecutorHTTPRequest(rawReq)
	case "executor.count_tokens":
		return handleExecutorCountTokens(rawReq)

	default:
		return nil, &dispatchError{code: "unknown_method", message: "unknown method: " + method}
	}
}

type dispatchError struct {
	code    string
	message string
}

func (e *dispatchError) Error() string   { return e.code + ": " + e.message }
func (e *dispatchError) Code() string    { return e.code }
func (e *dispatchError) Message() string { return e.message }

func registerPayload() string {
	return `{
  "schema_version": 2,
  "metadata": {
    "Name": "KiloCode Free",
    "Description": "Free models from KiloCode inference API",
    "Version": "0.1.13",
    "Prefix": "kilo-free",
    "Author": "kepeto",
    "GitHubRepository": "https://github.com/kepeto/cliproxyapi-plugins",
    "Logo": "https://kilo.ai/favicon.ico",
    "ConfigFields": [
      {"Name": "kilo_chat_url", "Type": "string", "Description": "KiloCode chat completions URL (default https://api.kilo.ai/api/gateway/chat/completions)"},
      {"Name": "kilo_models_url", "Type": "string", "Description": "KiloCode models URL (default https://api.kilo.ai/api/gateway/models)"}
    ]
  },
  "capabilities": {
    "auth_provider": true,
    "model_provider": true,
    "executor": true,
    "executor_model_scope": "both",
    "executor_input_formats": ["chat-completions"],
    "executor_output_formats": ["chat-completions"]
  }
}`
}

func okEnvelopeJSON(result string) ([]byte, error) {
	return json.Marshal(envelope{OK: true, Result: json.RawMessage(result)})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func errorEnvelopeWithStatus(code, message string, status int) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message, HTTPStatus: status}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.malloc(C.size_t(len(raw)))
	if ptr == nil {
		return
	}
	slice := (*[1 << 30]byte)(ptr)[:len(raw):len(raw)]
	copy(slice, raw)
	response.ptr = (*C.uint8_t)(ptr)
	response.len = C.size_t(len(raw))
}
