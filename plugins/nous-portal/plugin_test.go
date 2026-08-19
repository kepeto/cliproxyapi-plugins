package main

import (
	"encoding/json"
	"testing"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

func TestRegisterPayload(t *testing.T) {
	payload := registerPayload()
	if len(payload) == 0 {
		t.Fatal("registerPayload() returned empty string")
	}

	var meta map[string]any
	if err := json.Unmarshal([]byte(payload), &meta); err != nil {
		t.Fatalf("registerPayload() returned invalid JSON: %v", err)
	}

	metadata, ok := meta["metadata"].(map[string]any)
	if !ok {
		t.Fatal("registerPayload() missing metadata")
	}

	name, ok := metadata["Name"].(string)
	if !ok || name == "" {
		t.Error("registerPayload() metadata missing Name")
	}

	prefix, ok := metadata["Prefix"].(string)
	if !ok || prefix == "" {
		t.Error("registerPayload() metadata missing Prefix")
	}

	version, ok := metadata["Version"].(string)
	if !ok || version == "" {
		t.Error("registerPayload() metadata missing Version")
	}

	configFields, ok := metadata["ConfigFields"].([]any)
	if !ok {
		t.Error("registerPayload() metadata missing ConfigFields")
	}

	// Check that prefix config field exists
	foundPrefix := false
	for _, cf := range configFields {
		field, ok := cf.(map[string]any)
		if !ok {
			continue
		}
		if field["Name"] == "prefix" {
			foundPrefix = true
			break
		}
	}
	if !foundPrefix {
		t.Error("registerPayload() ConfigFields missing prefix field")
	}
}

func TestPluginPrefixDefault(t *testing.T) {
	// Default prefix should be the plugin ID
	expected := "nous-portal"
	if currentPrefix() != expected {
		t.Errorf("currentPrefix() = %q, want %q", currentPrefix(), expected)
	}
}

func TestPluginPrefixOverride(t *testing.T) {
	// Save original prefix
	orig := currentPrefix()

	// Override prefix
	setPluginPrefix("custom-prefix")
	if currentPrefix() != "custom-prefix" {
		t.Errorf("currentPrefix() = %q, want %q", currentPrefix(), "custom-prefix")
	}

	// Verify prefixedModelID uses new prefix
	got := prefixedModelID("tencent/hy3:free")
	if got != "custom-prefix/tencent/hy3:free" {
		t.Errorf("prefixedModelID() = %q, want %q", got, "custom-prefix/tencent/hy3:free")
	}

	// Verify stripModelPrefix uses new prefix
	got = stripModelPrefix("custom-prefix/tencent/hy3:free")
	if got != "tencent/hy3:free" {
		t.Errorf("stripModelPrefix() = %q, want %q", got, "tencent/hy3:free")
	}

	// Restore original prefix
	setPluginPrefix(orig)
}

func TestPrefixedModelID(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"tencent/hy3:free", "nous-portal/tencent/hy3:free"},
		{"", ""},
	}

	for _, tt := range tests {
		got := prefixedModelID(tt.modelID)
		if got != tt.expected {
			t.Errorf("prefixedModelID(%q) = %q, want %q", tt.modelID, got, tt.expected)
		}
	}
}

func TestStripModelPrefix(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"nous-portal/tencent/hy3:free", "tencent/hy3:free"},
		{"tencent/hy3:free", "tencent/hy3:free"},
		{"", ""},
	}

	for _, tt := range tests {
		got := stripModelPrefix(tt.modelID)
		if got != tt.expected {
			t.Errorf("stripModelPrefix(%q) = %q, want %q", tt.modelID, got, tt.expected)
		}
	}
}

func TestPrefixSetAndGet(t *testing.T) {
	p := shared.NewPrefix("test")
	if p.Value() != "test" {
		t.Errorf("Prefix.Value() = %q, want %q", p.Value(), "test")
	}

	p.Set("new-test")
	if p.Value() != "new-test" {
		t.Errorf("Prefix.Value() after Set() = %q, want %q", p.Value(), "new-test")
	}

	got := p.Prefixed("model")
	if got != "new-test/model" {
		t.Errorf("Prefix.Prefixed() = %q, want %q", got, "new-test/model")
	}
}
