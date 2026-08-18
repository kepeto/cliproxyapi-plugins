package main

import (
	"encoding/base64"
	"strings"
)

// trimHTTP normalizes a base URL: trims whitespace and trailing slashes.
func trimHTTP(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimRight(v, "/")
}

// base64encode encodes raw bytes as standard base64
func base64encode(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}
