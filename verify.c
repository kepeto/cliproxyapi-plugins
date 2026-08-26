// Pure-C harness that loads one compiled plugin and drives the CLIProxyAPI
// plugin protocol through the initialized plugin API. One process verifies one
// shared object; the process exits without dlclose so an embedded Go runtime is
// never unmapped while its goroutines are still alive.
//
// Build: gcc -O2 -o verify verify.c -ldl
// Usage: ./verify [path/to/plugin.so]
#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// This is the canonical plugin ABI used by the production plugins.
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

typedef int (*cliproxy_plugin_init_fn)(cliproxy_host_api*, cliproxy_plugin_api*);

static int failures = 0;
static cliproxy_plugin_api* active_plugin;

static void check(int condition, const char* label) {
    if (condition) {
        printf("OK  %s\n", label);
    } else {
        printf("FAIL %s\n", label);
        failures++;
    }
}

static const char* json_key_value(const char* json, const char* key) {
    char pattern[256];
    snprintf(pattern, sizeof(pattern), "\"%s\"", key);
    const char* p = strstr(json, pattern);
    if (!p) return NULL;
    p += strlen(pattern);
    while (*p == ' ' || *p == '\t' || *p == '\r' || *p == '\n' || *p == ':') p++;
    return p;
}

static int json_bool(const char* json, const char* key) {
    const char* p = json_key_value(json, key);
    if (!p) return -1;
    if (strncmp(p, "true", 4) == 0) return 1;
    if (strncmp(p, "false", 5) == 0) return 0;
    return -1;
}

static int json_int_eq(const char* json, const char* key, long want) {
    const char* p = json_key_value(json, key);
    if (!p) return 0;
    char* end = NULL;
    long got = strtol(p, &end, 10);
    return end != p && got == want;
}

static int json_str_eq(const char* json, const char* key, const char* want) {
    const char* p = json_key_value(json, key);
    if (!p || *p != '"') return 0;
    p++;
    size_t want_len = strlen(want);
    return strncmp(p, want, want_len) == 0 && p[want_len] == '"';
}

static int json_nonempty_string(const char* json, const char* key) {
    const char* p = json_key_value(json, key);
    if (!p || *p != '"' || p[1] == '"') return 0;
    return strchr(p + 1, '"') != NULL;
}

static char* invoke(const char* method, const char* request, size_t request_len, int* status_out) {
    cliproxy_buffer response;
    response.ptr = NULL;
    response.len = 0;
    int status = active_plugin->call(method, (const uint8_t*)request, request_len, &response);
    if (status_out) *status_out = status;
    if (!response.ptr) return NULL;

    char* copy = malloc(response.len + 1);
    if (!copy) {
        active_plugin->free_buffer(response.ptr, response.len);
        return NULL;
    }
    memcpy(copy, response.ptr, response.len);
    copy[response.len] = '\0';
    active_plugin->free_buffer(response.ptr, response.len);
    return copy;
}

int main(int argc, char** argv) {
    const char* so = argc > 1 ? argv[1] : "plugins/nous-portal/nous-portal.so";
    printf("Verifying %s\n", so);

    void* handle = dlopen(so, RTLD_NOW | RTLD_LOCAL);
    if (!handle) {
        fprintf(stderr, "dlopen %s failed: %s\n", so, dlerror());
        return 2;
    }

    cliproxy_plugin_init_fn init = (cliproxy_plugin_init_fn)dlsym(handle, "cliproxy_plugin_init");
    void* direct_call = dlsym(handle, "cliproxyPluginCall");
    void* direct_free = dlsym(handle, "cliproxyPluginFree");
    void* direct_shutdown = dlsym(handle, "cliproxyPluginShutdown");
    if (!init || !direct_call || !direct_free || !direct_shutdown) {
        fprintf(stderr, "missing exported plugin symbol\n");
        return 2;
    }

    cliproxy_host_api host;
    memset(&host, 0, sizeof(host));
    host.abi_version = 1;

    cliproxy_plugin_api plugin;
    memset(&plugin, 0, sizeof(plugin));
    int init_status = init(&host, &plugin);
    check(init_status == 0, "plugin init returns zero");
    check(plugin.abi_version == 1, "plugin ABI version=1");
    check(plugin.call != NULL, "plugin callback is populated");
    check(plugin.free_buffer != NULL, "plugin free callback is populated");
    check(plugin.shutdown != NULL, "plugin shutdown callback is populated");
    if (init_status != 0 || !plugin.call || !plugin.free_buffer || !plugin.shutdown) {
        return 1;
    }
    active_plugin = &plugin;

    int status = 0;
    char* registered = invoke("plugin.register", NULL, 0, &status);
    check(status == 0, "plugin.register call returns zero");
    check(registered != NULL, "plugin.register returns response");
    if (registered) {
        check(json_bool(registered, "ok") == 1, "plugin.register ok=true");
        check(json_int_eq(registered, "schema_version", 2), "schema_version=2");
        check(json_nonempty_string(registered, "Name"), "metadata.Name nonempty");
        check(json_nonempty_string(registered, "Version"), "metadata.Version nonempty");
        check(json_nonempty_string(registered, "Prefix"), "metadata.Prefix nonempty");
        free(registered);
    }

    char* reconfigured = invoke("plugin.reconfigure", NULL, 0, &status);
    check(reconfigured != NULL, "plugin.reconfigure returns response");
    if (reconfigured) {
        check(json_bool(reconfigured, "ok") == 1, "plugin.reconfigure ok=true");
        free(reconfigured);
    }

    char* auth_identifier = invoke("auth.identifier", NULL, 0, &status);
    check(auth_identifier != NULL, "auth.identifier returns response");
    if (auth_identifier) {
        check(json_bool(auth_identifier, "ok") == 1, "auth.identifier ok=true");
        check(json_nonempty_string(auth_identifier, "Identifier"), "auth.identifier nonempty");
        free(auth_identifier);
    }

    char* executor_identifier = invoke("executor.identifier", NULL, 0, &status);
    check(executor_identifier != NULL, "executor.identifier returns response");
    if (executor_identifier) {
        check(json_bool(executor_identifier, "ok") == 1, "executor.identifier ok=true");
        check(json_nonempty_string(executor_identifier, "Identifier"), "executor.identifier nonempty");
        free(executor_identifier);
    }

    const char* foreign_auth = "{\"type\":\"openai\",\"api_key\":\"x\"}";
    char* parsed = invoke("auth.parse", foreign_auth, strlen(foreign_auth), &status);
    check(parsed != NULL, "auth.parse foreign returns response");
    if (parsed) {
        check(json_bool(parsed, "Handled") == 0, "auth.parse foreign Handled=false");
        free(parsed);
    }

    const char* empty_request = "{}";
    char* execute = invoke("executor.execute", empty_request, strlen(empty_request), &status);
    check(execute != NULL, "executor.execute invalid request returns response");
    if (execute) {
        check(json_bool(execute, "ok") == 0, "executor.execute invalid request ok=false");
        free(execute);
    }

    char* unknown = invoke("unknown.method", NULL, 0, &status);
    check(unknown != NULL, "unknown method returns response");
    if (unknown) {
        check(json_bool(unknown, "ok") == 0, "unknown method ok=false");
        check(json_str_eq(unknown, "code", "unknown_method"), "unknown method code");
        free(unknown);
    }

    active_plugin->shutdown();

    // Deliberately do not call dlclose(handle): a Go c-shared runtime may still
    // own goroutines at shutdown, and unmapping it here recreates the hotswap
    // SIGSEGV documented by this repository.
    (void)handle;
    if (failures == 0) {
        printf("ALL CHECKS PASSED\n");
        return 0;
    }
    printf("%d CHECK(S) FAILED\n", failures);
    return 1;
}
