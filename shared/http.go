package shared

import (
	"encoding/base64"
	"strings"
)

// TrimHTTP normalizes a base URL: trims whitespace and trailing slashes.
func TrimHTTP(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimRight(v, "/")
}

// Base64Encode encodes raw bytes as standard base64.
func Base64Encode(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Base64Decode decodes standard base64 bytes.
func Base64Decode(b []byte) ([]byte, error) {
	return base64.StdEncoding.DecodeString(string(b))
}
