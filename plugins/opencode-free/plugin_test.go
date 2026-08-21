package main

import (
	"encoding/json"
	"testing"
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
	expected := "opencode-free"
	if currentPrefix() != expected {
		t.Errorf("currentPrefix() = %q, want %q", currentPrefix(), expected)
	}
}

func TestPluginPrefixOverride(t *testing.T) {
	orig := currentPrefix()
	setPluginPrefix("custom-prefix")
	if currentPrefix() != "custom-prefix" {
		t.Errorf("currentPrefix() = %q, want %q", currentPrefix(), "custom-prefix")
	}
	setPluginPrefix(orig)
}

func TestPrefixedModelID(t *testing.T) {
	got := prefixedModelID("deepseek-v4-flash-free")
	if got != "opencode-free/deepseek-v4-flash-free" {
		t.Errorf("prefixedModelID() = %q, want %q", got, "opencode-free/deepseek-v4-flash-free")
	}
}

func TestStripModelPrefix(t *testing.T) {
	got := stripModelPrefix("opencode-free/deepseek-v4-flash-free")
	if got != "deepseek-v4-flash-free" {
		t.Errorf("stripModelPrefix() = %q, want %q", got, "deepseek-v4-flash-free")
	}
}

func TestResolveConfigYAML(t *testing.T) {
	cfg := resolveConfig([]byte("enabled: true\nmodel_aliases:\n    ox-alpha: x-preview-f-free\n"))
	if cfg.ModelAliases["ox-alpha"] != "x-preview-f-free" {
		t.Fatalf("model_aliases not parsed from YAML: %v", cfg.ModelAliases)
	}
}
