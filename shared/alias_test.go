package shared

import "testing"

func TestAliasTable(t *testing.T) {
	a := NewAliasTable()
	a.SetConfig(map[string]string{
		"ox-alpha": "x-preview-f-free",
		"":         "ignored",
		"self":     "self",
	})
	a.SetHost(map[string]string{"hy3": "tencent/hy3:free"})
	if got := a.Resolve("ox-alpha"); got != "x-preview-f-free" {
		t.Errorf("config alias broken: %q", got)
	}
	if got := a.Resolve("hy3"); got != "tencent/hy3:free" {
		t.Errorf("host alias broken: %q", got)
	}
	if got := a.Resolve("nemotron-3-ultra-free"); got != "nemotron-3-ultra-free" {
		t.Errorf("passthrough broken: %q", got)
	}
	if len(a.Entries()) != 2 {
		t.Errorf("Entries() = %d, want 2 (empty/self dropped)", len(a.Entries()))
	}
	// Config wins on conflict.
	a.SetHost(map[string]string{"ox-alpha": "other"})
	if got := a.Resolve("ox-alpha"); got != "x-preview-f-free" {
		t.Errorf("precedence broken: %q", got)
	}
	a.SetConfig(nil)
	a.SetHost(nil)
	if got := a.Resolve("ox-alpha"); got != "ox-alpha" {
		t.Errorf("clear broken")
	}
}

func TestHostModelAliases(t *testing.T) {
	raw := []byte(`{"Provider":"nous-portal-free","RawJSON":"e30=","Host":{"AuthDir":"/x","OAuthModelAlias":{"nous-portal-free":[{"Name":"tencent/hy3:free","Alias":"hy3"}]}}}`)
	got, ok := HostModelAliases(raw, "nous-portal-free")
	if !ok || got["hy3"] != "tencent/hy3:free" {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, ok := HostModelAliases([]byte(`{}`), "nous-portal-free"); ok {
		t.Fatal("missing host must report false")
	}
}
