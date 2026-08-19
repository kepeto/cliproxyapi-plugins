package main

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

// trimHTTP normalizes a base URL: trims whitespace and trailing slashes.
func trimHTTP(v string) string {
	return shared.TrimHTTP(v)
}

// base64encode encodes raw bytes as standard base64 (matches Go []byte JSON marshaling).
func base64encode(b []byte) string {
	return shared.Base64Encode(b)
}

func randomState() string {
	return shared.RandomState()
}

// httpPostForm performs an OAuth form-post and decodes the JSON body into out.
func httpPostForm(portalBaseURL, path string, values map[string]string, timeout time.Duration) (int, []byte, error) {
	form := make([]string, 0, len(values))
	for k, v := range values {
		form = append(form, k+"="+shared.URLEncode(v))
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
	return shared.URLEncode(v)
}

func hexOf(c byte) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[c>>4], digits[c&0x0F]})
}
