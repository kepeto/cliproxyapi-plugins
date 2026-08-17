package main

/*
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <dlfcn.h>

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

static void* open_plugin(const char* path) {
	void* h = dlopen(path, RTLD_NOW | RTLD_LOCAL);
	if (!h) {
		const char* e = dlerror();
		if (e) fprintf(stderr, "dlopen error: %s\n", e);
	}
	return h;
}
static void* sym_plugin(void* h, const char* name) { return dlsym(h, name); }
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"os"
	"unsafe"
)

const soPath = "nous-portal.so"

func openLib() unsafe.Pointer {
	cpath := C.CString(soPath)
	defer C.free(unsafe.Pointer(cpath))
	h := C.open_plugin(cpath)
	if h == nil {
		fmt.Fprintf(os.Stderr, "dlopen %s failed\n", soPath)
		os.Exit(1)
	}
	return h
}

func mustSym(h unsafe.Pointer, name string) unsafe.Pointer {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	sym := C.sym_plugin(h, cname)
	if sym == nil {
		fmt.Fprintf(os.Stderr, "dlsym %s failed\n", name)
		os.Exit(1)
	}
	return sym
}

func main() {
	h := openLib()
	var symInit, symCall, symFree, symShutdown unsafe.Pointer
	symInit = mustSym(h, "cliproxy_plugin_init")
	symCall = mustSym(h, "cliproxyPluginCall")
	symFree = mustSym(h, "cliproxyPluginFree")
	symShutdown = mustSym(h, "cliproxyPluginShutdown")
	initFn := *(*func(*C.cliproxy_host_api, *C.cliproxy_plugin_api) C.int)(unsafe.Pointer(&symInit))
	callFn := *(*func(*C.char, *C.uint8_t, C.size_t, *C.cliproxy_buffer) C.int)(unsafe.Pointer(&symCall))
	freeFn := *(*func(unsafe.Pointer, C.size_t))(unsafe.Pointer(&symFree))
	shutdownFn := *(*func())(unsafe.Pointer(&symShutdown))

	var ha C.cliproxy_host_api
	var pa C.cliproxy_plugin_api
	if rc := initFn(&ha, &pa); int(rc) != 0 {
		fmt.Fprintln(os.Stderr, "cliproxy_plugin_init failed")
		os.Exit(1)
	}
	if pa.abi_version != 1 {
		fmt.Fprintf(os.Stderr, "abi_version=%d want 1\n", pa.abi_version)
		os.Exit(1)
	}
	defer shutdownFn()

	call := func(method string, req []byte) ([]byte, int) {
		cmethod := C.CString(method)
		defer C.free(unsafe.Pointer(cmethod))
		var creq *C.uint8_t
		if len(req) > 0 {
			creq = (*C.uint8_t)(unsafe.Pointer(&req[0]))
		}
		var resp C.cliproxy_buffer
		rc := callFn(cmethod, creq, C.size_t(len(req)), &resp)
		var out []byte
		if resp.ptr != nil && resp.len > 0 {
			out = C.GoBytes(unsafe.Pointer(resp.ptr), C.int(resp.len))
			freeFn(unsafe.Pointer(resp.ptr), resp.len)
		}
		return out, int(rc)
	}

	fail := func(msg string, args ...any) {
		fmt.Fprintf(os.Stderr, "FAIL: "+msg+"\n", args...)
		os.Exit(1)
	}

	// plugin.register
	raw, rc := call("plugin.register", nil)
	if rc != 0 {
		fail("register rc=%d", rc)
	}
	var regEnv struct {
		OK     bool `json:"ok"`
		Result struct {
			SchemaVersion uint32 `json:"schema_version"`
			Metadata      struct {
				Name    string `json:"Name"`
				Version string `json:"Version"`
			} `json:"metadata"`
			Capabilities struct {
				AuthProvider  bool     `json:"auth_provider"`
				ModelProvider bool     `json:"model_provider"`
				Executor      bool     `json:"executor"`
				ExecutorScope string   `json:"executor_model_scope"`
				InFormats     []string `json:"executor_input_formats"`
				OutFormats    []string `json:"executor_output_formats"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &regEnv); err != nil {
		fail("register decode: %v", err)
	}
	if !regEnv.OK || !regEnv.Result.Capabilities.AuthProvider || !regEnv.Result.Capabilities.ModelProvider || !regEnv.Result.Capabilities.Executor {
		fail("capabilities missing: %+v", regEnv.Result.Capabilities)
	}
	if regEnv.Result.Capabilities.ExecutorScope != "oauth" {
		fail("executor_model_scope=%q", regEnv.Result.Capabilities.ExecutorScope)
	}
	if len(regEnv.Result.Capabilities.InFormats) != 1 || regEnv.Result.Capabilities.InFormats[0] != "chat-completions" {
		fail("in_formats=%v", regEnv.Result.Capabilities.InFormats)
	}
	fmt.Printf("OK register: name=%s version=%s scope=%s\n", regEnv.Result.Metadata.Name, regEnv.Result.Metadata.Version, regEnv.Result.Capabilities.ExecutorScope)

	// identifiers
	for _, m := range []string{"auth.identifier", "executor.identifier"} {
		raw, rc := call(m, nil)
		if rc != 0 {
			fail("%s rc=%d", m, rc)
		}
		var env struct {
			OK     bool `json:"ok"`
			Result struct {
				Identifier string `json:"Identifier"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			fail("%s decode: %v", m, err)
		}
		if !env.OK || env.Result.Identifier != "nous-portal" {
			fail("%s identifier=%q", m, env.Result.Identifier)
		}
		fmt.Printf("OK %s -> %s\n", m, env.Result.Identifier)
	}

	// model.static
	raw, rc = call("model.static", nil)
	if rc != 0 {
		fail("model.static rc=%d", rc)
	}
	var staticEnv struct {
		OK     bool `json:"ok"`
		Result struct {
			Provider string `json:"Provider"`
			Models   []struct {
				ID string `json:"ID"`
			} `json:"Models"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &staticEnv); err != nil {
		fail("model.static decode: %v", err)
	}
	if !staticEnv.OK || staticEnv.Result.Provider != "nous-portal" {
		fail("model.static provider=%q", staticEnv.Result.Provider)
	}
	if len(staticEnv.Result.Models) == 0 {
		fail("model.static no models")
	}
	fmt.Printf("OK model.static: %d models first=%s\n", len(staticEnv.Result.Models), staticEnv.Result.Models[0].ID)

	// auth.parse foreign
	raw, rc = call("auth.parse", []byte(`{"type":"openai","api_key":"x"}`))
	if rc != 0 {
		fail("auth.parse foreign rc=%d", rc)
	}
	var foreignEnv struct {
		OK     bool `json:"ok"`
		Result struct {
			Handled bool `json:"Handled"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &foreignEnv); err != nil {
		fail("auth.parse foreign decode: %v", err)
	}
	if !foreignEnv.OK || foreignEnv.Result.Handled {
		fail("expected Handled=false for foreign auth")
	}
	fmt.Println("OK auth.parse foreign -> Handled=false")

	// auth.parse nous-portal
	authFile := []byte(`{"type":"nous-portal","access_token":"at","refresh_token":"rt","expires_at":"2030-01-01T00:00:00Z","portal_base_url":"https://portal.nousresearch.com","inference_base_url":"https://inference-api.nousresearch.com/v1","client_id":"hermes-cli","scope":"inference:invoke"}`)
	raw, rc = call("auth.parse", authFile)
	if rc != 0 {
		fail("auth.parse nous rc=%d", rc)
	}
	var nousEnv struct {
		OK     bool `json:"ok"`
		Result struct {
			Handled bool `json:"Handled"`
			Auth    struct {
				Provider    string `json:"Provider"`
				StorageJSON string `json:"StorageJSON"`
			} `json:"Auth"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &nousEnv); err != nil {
		fail("auth.parse nous decode: %v", err)
	}
	if !nousEnv.OK || !nousEnv.Result.Handled {
		fail("expected Handled=true for nous-portal auth")
	}
	if nousEnv.Result.Auth.Provider != "nous-portal" {
		fail("provider=%q", nousEnv.Result.Auth.Provider)
	}
	fmt.Println("OK auth.parse nous-portal -> Handled=true")

	// executor.execute missing auth
	payload, _ := json.Marshal(map[string]any{
		"Model":       "openai/gpt-5.5",
		"Stream":      false,
		"Payload":     []byte(`{"model":"openai/gpt-5.5","messages":[]}`),
		"StorageJSON": []byte(`{}`),
	})
	raw, rc = call("executor.execute", payload)
	if rc != 0 {
		fail("executor.execute rc=%d", rc)
	}
	var execEnv struct {
		OK    bool `json:"ok"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &execEnv); err != nil {
		fail("executor.execute decode: %v", err)
	}
	if execEnv.OK || execEnv.Error == nil || execEnv.Error.Code != "auth_required" {
		fail("expected auth_required error, got %s", raw)
	}
	fmt.Println("OK executor.execute missing-auth -> auth_required")

	fmt.Println("\nALL CHECKS PASSED")
}
