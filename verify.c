// Pure-C harness that dlopen()s the compiled plugin and drives the CLIProxyAPI
// plugin protocol (cliproxy_plugin_init + cliproxyPluginCall). Validates the wire
// contract without a second Go runtime in the process (which would crash).
//
// Build: gcc -o verify verify.c -ldl && ./verify
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <dlfcn.h>

typedef struct {
    void* ptr;
    size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);


typedef struct {
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

typedef int (*cliproxy_plugin_init_fn)(const cliproxy_host_api*, cliproxy_plugin_api*);
#ifndef SO
#define SO "nous-portal.so"
#endif

static int failures = 0;

// Count occurrences of a substring (used to count model entries by "ID": keys).
static int count_substr(const char* hay, const char* needle) {
    int n = 0;
    if (!hay || !needle) return 0;
    size_t nl = strlen(needle);
    const char* p = hay;
    while ((p = strstr(p, needle)) != NULL) {
        n++;
        p += nl;
    }
    return n;
}

static int json_int(const char* json, const char* key, long* out) {
    char pat[256];
    snprintf(pat, sizeof(pat), "\"%s\"", key);
    const char* p = strstr(json, pat);
    if (!p) return 0;
    p += strlen(pat);
    while (*p && (*p == ' ' || *p == ':')) p++;
    char* end;
    long v = strtol(p, &end, 10);
    if (end == p) return 0;
    *out = v;
    return 1;
}

// Check integer value of "key":<int>.
static int json_int_eq(const char* json, const char* key, long want) {
    long v = 0;
    return json_int(json, key, &v) && v == want;
}

// Minimal JSON substring search: find "key":value (string) and compare.
static int json_str_eq(const char* json, const char* key, const char* want) {
    char pat[256];
    snprintf(pat, sizeof(pat), "\"%s\"", key);
    const char* p = strstr(json, pat);
    if (!p) return 0;
    p += strlen(pat);
    while (*p && (*p == ' ' || *p == ':')) p++;
    if (*p != '"') return 0;
    p++;
    return strncmp(p, want, strlen(want)) == 0 && p[strlen(want)] == '"';
}

static int json_bool(const char* json, const char* key) {
    char pat[256];
    snprintf(pat, sizeof(pat), "\"%s\"", key);
    const char* p = strstr(json, pat);
    if (!p) return -1;
    p += strlen(pat);
    while (*p && (*p == ' ' || *p == ':')) p++;
    if (strncmp(p, "true", 4) == 0) return 1;
    if (strncmp(p, "false", 5) == 0) return 0;
    return -1;
}


static void check(int cond, const char* label) {
    if (cond) {
        printf("OK  %s\n", label);
    } else {
        printf("FAIL %s\n", label);
        failures++;
    }
}

int main(void) {
    void* h = dlopen(SO, RTLD_NOW | RTLD_LOCAL);
    if (!h) {
        fprintf(stderr, "dlopen %s failed: %s\n", SO, dlerror());
        return 2;
    }

    cliproxy_plugin_init_fn init = (cliproxy_plugin_init_fn)dlsym(h, "cliproxy_plugin_init");
    cliproxy_plugin_call_fn call = (cliproxy_plugin_call_fn)dlsym(h, "cliproxyPluginCall");
    cliproxy_plugin_free_fn free_fn = (cliproxy_plugin_free_fn)dlsym(h, "cliproxyPluginFree");
    cliproxy_plugin_shutdown_fn shutdown = (cliproxy_plugin_shutdown_fn)dlsym(h, "cliproxyPluginShutdown");
    if (!init || !call || !free_fn || !shutdown) {
        fprintf(stderr, "missing symbol\n");
        return 2;
    }

    cliproxy_host_api host_api;
    memset(&host_api, 0, sizeof(host_api));
    cliproxy_plugin_api plugin_api;
    memset(&plugin_api, 0, sizeof(plugin_api));
    if (init(&host_api, &plugin_api) != 0) {
        fprintf(stderr, "init failed\n");
        return 2;
    }
    if (plugin_api.abi_version != 1) {
        fprintf(stderr, "abi_version=%u want 1\n", plugin_api.abi_version);
        return 2;
    }

    char* invoke(const char* method, const char* req, size_t reqlen) {
        cliproxy_buffer resp;
        resp.ptr = NULL;
        resp.len = 0;
        call((char*)method, req ? (const uint8_t*)req : NULL, reqlen, &resp);
        if (!resp.ptr) return NULL;
        char* out = malloc(resp.len + 1);
        memcpy(out, resp.ptr, resp.len);
        out[resp.len] = 0;
        free_fn(resp.ptr, resp.len);
        return out;
    }

    // plugin.register
    char* r = invoke("plugin.register", NULL, 0);
    check(r != NULL, "plugin.register returns");
    if (r) {
        check(json_bool(r, "ok") == 1, "register ok=true");
        check(json_int_eq(r, "schema_version", 3), "schema_version=3");
        check(json_str_eq(r, "Name", "nous-portal"), "metadata.Name=nous-portal");
        check(json_bool(r, "auth_provider") == 1, "capabilities.auth_provider");
        check(json_bool(r, "model_provider") == 1, "capabilities.model_provider");
        check(json_bool(r, "executor") == 1, "capabilities.executor");
        check(json_str_eq(r, "executor_model_scope", "oauth"), "executor_model_scope=oauth");
        free(r);
    }

    // identifiers
    const char* ids[] = {"auth.identifier", "executor.identifier"};
    for (int i = 0; i < 2; i++) {
        char* ri = invoke(ids[i], NULL, 0);
        check(ri && json_str_eq(ri, "Identifier", "nous-portal"), ids[i]);
        free(ri);
    }

    // model.static
    char* ms = invoke("model.static", NULL, 0);
    check(ms && json_str_eq(ms, "Provider", "nous-portal"), "model.static Provider=nous-portal");
    int mcount = ms ? count_substr(ms, "\"ID\":") : 0;
    check(mcount > 0, "model.static has models");
    free(ms);

    // auth.parse foreign -> Handled=false
    const char* foreign = "{\"type\":\"openai\",\"api_key\":\"x\"}";
    char* apf = invoke("auth.parse", foreign, strlen(foreign));
    check(apf && json_bool(apf, "Handled") == 0, "auth.parse foreign Handled=false");
    free(apf);

    // auth.parse nous-portal -> Handled=true, Provider=nous-portal
    const char* nous = "{\"type\":\"nous-portal\",\"access_token\":\"at\",\"refresh_token\":\"rt\",\"expires_at\":\"2030-01-01T00:00:00Z\",\"portal_base_url\":\"https://portal.nousresearch.com\",\"inference_base_url\":\"https://inference-api.nousresearch.com/v1\",\"client_id\":\"hermes-cli\",\"scope\":\"inference:invoke\"}";
    char* apn = invoke("auth.parse", nous, strlen(nous));
    check(apn && json_bool(apn, "Handled") == 1, "auth.parse nous Handled=true");
    // Provider appears inside result.Auth.Provider
    if (apn) {
        char* auth = strstr(apn, "\"Auth\"");
        check(auth && json_str_eq(auth, "Provider", "nous-portal"), "auth.parse Auth.Provider=nous-portal");
    }
    free(apn);

    // executor.execute missing auth -> error code auth_required
    const char* execreq = "{\"Model\":\"openai/gpt-5.5\",\"Stream\":false,\"Payload\":\"eyJtb2RlbCI6Im9wZW5haS9ncHQtNS41IiwibWVzc2FnZXMiOltdfQ==\",\"StorageJSON\":\"e30=\"}";
    char* ex = invoke("executor.execute", execreq, strlen(execreq));
    check(ex != NULL, "executor.execute returns");
    if (ex) {
        // error envelope: ok=false, error.code=auth_required
        int ok = json_bool(ex, "ok");
        char* err = strstr(ex, "\"error\"");
        check(ok == 0, "executor.execute ok=false (missing auth)");
        check(err && json_str_eq(err, "code", "auth_required"), "executor.execute error.code=auth_required");
        free(ex);
    }

    shutdown();

    if (failures == 0) {
        printf("\nALL CHECKS PASSED\n");
        return 0;
    }
    printf("\n%d CHECK(S) FAILED\n", failures);
    return 1;
}
