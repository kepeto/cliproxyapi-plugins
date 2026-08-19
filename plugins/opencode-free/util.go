package main

import (
	"github.com/kepeto/cliproxyapi-plugins/shared"
)

// trimHTTP normalizes a base URL: trims whitespace and trailing slashes.
func trimHTTP(v string) string {
	return shared.TrimHTTP(v)
}

// base64encode encodes raw bytes as standard base64
func base64encode(b []byte) string {
	return shared.Base64Encode(b)
}

// safeTruncate returns up to n bytes of b as a string, avoiding slice-bounds panics.
func safeTruncate(b []byte, n int) string {
	if n <= 0 || len(b) == 0 {
		return ""
	}
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}
