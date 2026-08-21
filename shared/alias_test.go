package shared

import "testing"

func TestAliasTable(t *testing.T) {
	a := NewAliasTable()
	a.Set(map[string]string{
		"ox-alpha": "x-preview-f-free",
		"":         "ignored",
		"self":     "self",
		"kimi-k2":  "moonshotai/kimi-k2:free",
	})
	if got := a.Resolve("ox-alpha"); got != "x-preview-f-free" {
		t.Errorf("Resolve(ox-alpha) = %q", got)
	}
	if got := a.Resolve("nemotron-3-ultra-free"); got != "nemotron-3-ultra-free" {
		t.Errorf("passthrough broken: %q", got)
	}
	if len(a.Entries()) != 2 {
		t.Errorf("Entries() = %d, want 2 (empty/self dropped)", len(a.Entries()))
	}
	a.Set(nil)
	if got := a.Resolve("ox-alpha"); got != "ox-alpha" {
		t.Errorf("Set(nil) did not clear aliases")
	}
}
