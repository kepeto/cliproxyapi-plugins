package main

import (
	"strings"
)

// trimHTTP normalizes a base URL: trims whitespace and trailing slashes.
func trimHTTP(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimRight(v, "/")
}

// base64encode encodes raw bytes as standard base64
func base64encode(b []byte) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	if len(b) == 0 {
		return ""
	}
	var out strings.Builder
	out.Grow((len(b) + 2) / 3 * 4)
	for i := 0; i < len(b); i += 3 {
		n0 := b[i]
		n1 := byte(0)
		n2 := byte(0)
		if i+1 < len(b) {
			n1 = b[i+1]
		}
		if i+2 < len(b) {
			n2 = b[i+2]
		}
		out.WriteByte(enc[n0>>2])
		out.WriteByte(enc[((n0&0x03)<<4)|(n1>>4)])
		if i+1 >= len(b) {
			out.WriteByte('=')
			out.WriteByte('=')
			break
		}
		out.WriteByte(enc[((n1&0x0f)<<1)|(n2>>7)])
		if i+2 >= len(b) {
			out.WriteByte('=')
			break
		}
		out.WriteByte(enc[((n2&0x3f)>>2)])
		out.WriteByte(enc[((n2&0x03)<<4)|(0)])
	}
	return out.String()
}
