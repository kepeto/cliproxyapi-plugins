package main

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"
)

// trimHTTP normalizes a base URL: trims whitespace and trailing slashes.
func trimHTTP(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimRight(v, "/")
}

// base64encode encodes raw bytes as standard base64 (matches Go []byte JSON marshaling).
func base64encode(b []byte) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	if len(b) == 0 {
		return ""
	}
	var out strings.Builder
	out.Grow((len(b) + 2) / 3 * 4)
	for i := 0; i < len(b); i += 3 {
		var n uint32
		n |= uint32(b[i]) << 16
		rem := 2
		if i+1 < len(b) {
			n |= uint32(b[i+1]) << 8
		} else {
			rem--
		}
		if i+2 < len(b) {
			n |= uint32(b[i+2])
		} else {
			rem--
		}
		out.WriteByte(enc[(n>>18)&0x3F])
		out.WriteByte(enc[(n>>12)&0x3F])
		if rem >= 1 {
			out.WriteByte(enc[(n>>6)&0x3F])
		} else {
			out.WriteByte('=')
		}
		if rem >= 2 {
			out.WriteByte(enc[n&0x3F])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}

func randomState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b)
}

// httpPostForm performs an OAuth form-post and decodes the JSON body into out.
func httpPostForm(portalBaseURL, path string, values map[string]string, timeout time.Duration) (int, []byte, error) {
	form := make([]string, 0, len(values))
	for k, v := range values {
		form = append(form, k+"="+urlEncode(v))
	}
	body := strings.Join(form, "&")
	req, err := http.NewRequest(http.MethodPost, portalBaseURL+path, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

// urlEncode does minimal form-encoding (no net/url import to keep deps tiny).
func urlEncode(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteString(hexOf(c))
		}
	}
	return b.String()
}

func hexOf(c byte) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[c>>4], digits[c&0x0F]})
}
