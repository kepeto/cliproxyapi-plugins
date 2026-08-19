package shared

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPrefix(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		modelID   string
		expected  string
		stripIn   string
		stripOut  string
	}{
		{
			name:     "basic prefix",
			prefix:   "nous-portal",
			modelID:  "tencent/hy3:free",
			expected: "nous-portal/tencent/hy3:free",
			stripIn:  "nous-portal/tencent/hy3:free",
			stripOut: "tencent/hy3:free",
		},
		{
			name:     "empty prefix",
			prefix:   "",
			modelID:  "tencent/hy3:free",
			expected: "tencent/hy3:free",
			stripIn:  "tencent/hy3:free",
			stripOut: "tencent/hy3:free",
		},
		{
			name:     "no prefix in model id",
			prefix:   "nous-portal",
			modelID:  "tencent/hy3:free",
			expected: "nous-portal/tencent/hy3:free",
			stripIn:  "tencent/hy3:free",
			stripOut: "tencent/hy3:free",
		},
		{
			name:     "empty model id",
			prefix:   "nous-portal",
			modelID:  "",
			expected: "",
			stripIn:  "",
			stripOut: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPrefix(tt.prefix)
			got := p.Prefixed(tt.modelID)
			if got != tt.expected {
				t.Errorf("Prefixed() = %v, want %v", got, tt.expected)
			}
			gotStrip := p.Strip(tt.stripIn)
			if gotStrip != tt.stripOut {
				t.Errorf("Strip() = %v, want %v", gotStrip, tt.stripOut)
			}
		})
	}
}

func TestPrefixStripFromPayload(t *testing.T) {
	p := NewPrefix("nous-portal")

	tests := []struct {
		name     string
		payload  []byte
		modelID  string
	}{
		{
			name:    "strip prefix from model",
			payload: []byte(`{"model":"nous-portal/tencent/hy3:free","messages":[]}`),
			modelID: "tencent/hy3:free",
		},
		{
			name:    "no prefix to strip",
			payload: []byte(`{"model":"tencent/hy3:free","messages":[]}`),
			modelID: "tencent/hy3:free",
		},
		{
			name:    "invalid json",
			payload: []byte(`not json`),
			modelID: "",
		},
		{
			name:    "empty payload",
			payload: []byte{},
			modelID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.StripFromPayload(tt.payload)
			if tt.modelID == "" {
				if string(got) != string(tt.payload) {
					t.Errorf("StripFromPayload() = %q, want %q (unchanged)", string(got), string(tt.payload))
				}
				return
			}
			var result map[string]any
			if err := json.Unmarshal(got, &result); err != nil {
				t.Fatalf("StripFromPayload() returned invalid JSON: %v", err)
			}
			model, ok := result["model"].(string)
			if !ok {
				t.Fatalf("StripFromPayload() missing model field")
			}
			if model != tt.modelID {
				t.Errorf("StripFromPayload() model = %q, want %q", model, tt.modelID)
			}
		})
	}
}

func TestTrimHTTP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://example.com", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{"https://example.com/path/", "https://example.com/path"},
		{"  https://example.com  ", "https://example.com"},
		{"", ""},
	}

	for _, tt := range tests {
		got := TrimHTTP(tt.input)
		if got != tt.expected {
			t.Errorf("TrimHTTP(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestBase64Encode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"hello", "aGVsbG8="},
		{"hello world", "aGVsbG8gd29ybGQ="},
	}

	for _, tt := range tests {
		got := Base64Encode([]byte(tt.input))
		if got != tt.expected {
			t.Errorf("Base64Encode(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestBase64Decode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"", "", false},
		{"aGVsbG8=", "hello", false},
		{"!!!invalid!!!", "", true},
	}

	for _, tt := range tests {
		got, err := Base64Decode([]byte(tt.input))
		if (err != nil) != tt.wantErr {
			t.Errorf("Base64Decode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if string(got) != tt.expected {
			t.Errorf("Base64Decode(%q) = %q, want %q", tt.input, string(got), tt.expected)
		}
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{-456, "-456"},
	}

	for _, tt := range tests {
		got := Itoa(tt.input)
		if got != tt.expected {
			t.Errorf("Itoa(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestURLEncode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello+world"},
		{"a/b", "a%2Fb"},
		{"a&b", "a%26b"},
		{"a=b", "a%3Db"},
	}

	for _, tt := range tests {
		got := URLEncode(tt.input)
		if got != tt.expected {
			t.Errorf("URLEncode(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		args     []string
		expected string
	}{
		{[]string{"a", "b"}, "a"},
		{[]string{"", "b"}, "b"},
		{[]string{"", ""}, ""},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		got := FirstNonEmpty(tt.args...)
		if got != tt.expected {
			t.Errorf("FirstNonEmpty(%v) = %q, want %q", tt.args, got, tt.expected)
		}
	}
}

func TestExpiryFromNow(t *testing.T) {
	got := ExpiryFromNow(3600)
	if got.IsZero() {
		t.Error("ExpiryFromNow() returned zero time")
	}
	now := time.Now()
	if got.Before(now) {
		t.Error("ExpiryFromNow() returned time in the past")
	}
}
