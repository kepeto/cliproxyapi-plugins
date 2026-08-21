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
	"fmt"

	"github.com/kepeto/cliproxyapi-plugins/shared"
	"unsafe"
)

// pluginVersion is injected at build time via -ldflags.
var pluginVersion = "dev"

// abiVersion must match pluginabi.ABIVersion (1).
const abiVersion uint32 = 1

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
			rawResp = shared.ErrorEnvelope(de.Code(), de.Message())
		} else {
			rawResp = shared.ErrorEnvelope("dispatch_error", err.Error())
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
		return shared.OKEnvelope(registerPayload())
	case "plugin.reconfigure":
		if len(rawReq) > 0 {
			cfg := resolveConfig(rawReq)
			setPluginPrefix(cfg.prefix())
		}
		return shared.OKEnvelope(registerPayload())

	case "auth.identifier":
		return shared.OKEnvelope(`{"Identifier":"` + PROVIDER_ID + `"}`)
	case "auth.parse":
		return handleAuthParse(rawReq)
	case "auth.login.start":
		return handleAuthLoginStart(rawReq)
	case "auth.login.poll":
		return handleAuthLoginPoll(rawReq)
	case "auth.refresh":
		return handleAuthRefresh(rawReq)

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
	return fmt.Sprintf(`{
  "schema_version": 2,
  "metadata": {
    "Name": "OpenCode Zen Free",
    "Description": "Free models from OpenCode Zen inference API",
    "Version": "%s",
    "Prefix": "%s",
    "Author": "kepeto",
    "GitHubRepository": "https://github.com/kepeto/cliproxyapi-plugins",
    "Logo": "https://opencode.ai/favicon.ico",
    "ConfigFields": [
      {"Name": "opencode_base_url", "Type": "string", "Description": "OpenCode Zen base URL (default https://opencode.ai/zen)"},
      {"Name": "prefix", "Type": "string", "Description": "Model ID prefix (default opencode-free)"}
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
}`, pluginVersion, currentPrefix())
}

func okEnvelopeJSON(result string) ([]byte, error) {
	return shared.OKEnvelope(result)
}

func errorEnvelope(code, message string) []byte {
	return shared.ErrorEnvelope(code, message)
}

func errorEnvelopeWithStatus(code, message string, status int) []byte {
	return shared.ErrorEnvelopeWithStatus(code, message, status)
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
