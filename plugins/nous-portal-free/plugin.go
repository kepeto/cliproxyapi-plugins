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

typedef int (*cliproxy_plugin_call_fn)(const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}


static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
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
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, shared.ErrorEnvelope("invalid_method", "method is required"))
		return 1
	}
	rawReq := copyCBuffer(request, requestLen)
	raw, errHandle := dispatch(C.GoString(method), rawReq)
	if errHandle != nil {
		writeResponse(response, shared.ErrorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	// Best-effort: drop any in-flight device-code polls.
	shutdownLoginPollers()
}

// copyCBuffer copies the request payload delivered by the host into a Go slice.
func copyCBuffer(ptr *C.uint8_t, length C.size_t) []byte {
	if ptr == nil || length == 0 {
		return nil
	}
	return C.GoBytes(unsafe.Pointer(ptr), C.int(length))
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
		return shared.OKEnvelope(`{"Identifier":"nous-portal-free"}`)
	case "auth.parse":
		return handleAuthParse(rawReq)
	case "auth.login.start":
		return handleAuthLoginStart(rawReq)
	case "auth.login.poll":
		return handleAuthLoginPoll(rawReq)
	case "auth.refresh":
		return handleAuthRefresh(rawReq)

	case "model.static":
		return shared.OKEnvelope(modelStaticPayload())
	case "model.for_auth":
		return handleModelForAuth(rawReq)

	case "executor.identifier":
		return shared.OKEnvelope(`{"Identifier":"nous-portal-free"}`)
	case "executor.execute":
		return handleExecutorExecute(rawReq)
	case "executor.execute_stream":
		return handleExecutorExecuteStream(rawReq)
	case "executor.count_tokens":
		return handleExecutorCountTokens(rawReq)
	case "executor.http_request":
		return handleExecutorHTTPRequest(rawReq)

	default:
		return shared.ErrorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func registerPayload() string {
	return fmt.Sprintf(`{
  "schema_version": 2,
  "metadata": {
    "Name": "Nous Portal Free",
    "Description": "Free models only from Nous Portal inference API",
    "Version": "%s",
    "Prefix": "%s",
    "Author": "kepeto",
    "GitHubRepository": "https://github.com/kepeto/cliproxyapi-plugins",
    "Logo": "https://hermes-agent.nousresearch.com/favicon.ico",
    "Icon": "https://hermes-agent.nousresearch.com/favicon.ico",
    "Homepage": "https://portal.nousresearch.com",
    "License": "MIT",
    "Tags": ["nous", "portal", "oauth", "inference"],
    "ConfigFields": [
      {"Name": "portal_base_url", "Type": "string", "Description": "Nous Portal OAuth base URL (default https://portal.nousresearch.com)"},
      {"Name": "inference_base_url", "Type": "string", "Description": "OpenAI-compatible inference base URL (default https://inference-api.nousresearch.com/v1)"},
      {"Name": "client_id", "Type": "string", "Description": "OAuth client id (default hermes-cli)"},
      {"Name": "scope", "Type": "string", "Description": "OAuth scope (default inference:invoke)"},
      {"Name": "prefix", "Type": "string", "Description": "Model ID prefix (default nous-portal-free)"}
    ]
  },
  "capabilities": {
    "auth_provider": true,
    "model_provider": true,
    "executor": true,
    "executor_model_scope": "oauth",
    "executor_input_formats": ["chat-completions"],
    "executor_output_formats": ["chat-completions"]
  }
}`, pluginVersion, currentPrefix())
}

// okEnvelopeJSON wraps a JSON string in a success envelope.
func okEnvelopeJSON(result string) ([]byte, error) {
	return shared.OKEnvelope(result)
}

// okEnvelopeJSONStr wraps a pre-serialized JSON result string into an envelope.
func okEnvelopeJSONStr(result string) ([]byte, error) {
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
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}
