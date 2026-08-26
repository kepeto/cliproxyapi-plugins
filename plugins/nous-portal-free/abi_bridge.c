#include <stddef.h>
#include <stdint.h>

typedef struct {
    void* ptr;
    size_t len;
} cliproxy_buffer;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);

int cliproxy_plugin_call_bridge(const char* method, const uint8_t* payload, size_t payload_len, cliproxy_buffer* response) {
    return cliproxyPluginCall((char*)method, (uint8_t*)payload, payload_len, response);
}
