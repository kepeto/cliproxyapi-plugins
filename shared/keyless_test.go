package shared

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func testKeylessAuth() KeylessAuth {
	return KeylessAuth{
		Provider: "example-free",
		Label:    "Example Free",
		Token:    "public",
		LoginURL: "https://example.com/",
	}
}

func TestKeylessAuthParseEnvelope(t *testing.T) {
	auth := testKeylessAuth()
	storage := []byte(`{"type":"example-free","access_token":"custom","disabled":true}`)
	result := auth.Parse([]byte(MustJSON(map[string]any{
		"FileName": "custom.json",
		"RawJSON":  storage,
	})))

	var parsed struct {
		Handled bool
		Auth    struct {
			ID          string
			FileName    string
			Disabled    bool
			StorageJSON []byte
		}
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.Handled || parsed.Auth.ID != auth.Provider || parsed.Auth.FileName != "custom.json" || !parsed.Auth.Disabled {
		t.Fatalf("unexpected parsed auth: %#v", parsed)
	}
	if string(parsed.Auth.StorageJSON) != string(storage) {
		t.Fatalf("storage = %q, want %q", parsed.Auth.StorageJSON, storage)
	}
}

func TestKeylessAuthEnsure(t *testing.T) {
	auth := testKeylessAuth()
	dir := t.TempDir()
	req := []byte(MustJSON(map[string]any{"Host": map[string]any{"AuthDir": dir}}))
	path := filepath.Join(dir, auth.Provider+".json")

	if err := auth.Ensure(req); err != nil {
		t.Fatal(err)
	}
	created, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := json.Unmarshal(created, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["type"] != auth.Provider || profile["access_token"] != auth.Token {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	firstID, _ := profile["profile_id"].(string)
	if firstID == "" {
		t.Fatal("profile_id is empty")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := auth.Ensure(req); err != nil {
		t.Fatal(err)
	}
	recreated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(recreated, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["profile_id"] == firstID {
		t.Fatal("recreated profile retained stale hash")
	}

	const existing = `{"type":"example-free","access_token":"keep","disabled":true}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := auth.Ensure(req); err != nil {
		t.Fatal(err)
	}
	preserved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved) != existing {
		t.Fatalf("existing profile overwritten: %s", preserved)
	}
}

func TestKeylessAuthLogin(t *testing.T) {
	auth := testKeylessAuth()
	dir := t.TempDir()
	host := map[string]any{"Host": map[string]any{"AuthDir": dir}}
	started, err := auth.StartLogin([]byte(MustJSON(host)))
	if err != nil {
		t.Fatal(err)
	}
	var start struct {
		State string
		URL   string
	}
	if err := json.Unmarshal([]byte(started), &start); err != nil {
		t.Fatal(err)
	}
	if start.State != auth.Provider+"-keyless" || start.URL != auth.LoginURL {
		t.Fatalf("unexpected login start: %#v", start)
	}

	polled, err := auth.PollLogin([]byte(MustJSON(map[string]any{
		"State": start.State,
		"Host":  host["Host"],
	})))
	if err != nil {
		t.Fatal(err)
	}
	var poll struct {
		Status string
		Auth   struct{ ID string }
	}
	if err := json.Unmarshal([]byte(polled), &poll); err != nil {
		t.Fatal(err)
	}
	if poll.Status != "success" || poll.Auth.ID != auth.Provider {
		t.Fatalf("unexpected login poll: %#v", poll)
	}
}

func TestKeylessAuthRefreshPreservesIdentity(t *testing.T) {
	auth := testKeylessAuth()
	storage := []byte(`{"type":"example-free","access_token":"keep","disabled":true}`)
	result := auth.Refresh([]byte(MustJSON(map[string]any{
		"AuthID":      "existing-id",
		"StorageJSON": storage,
	})))
	var refreshed struct {
		Auth struct {
			ID          string
			Disabled    bool
			StorageJSON []byte
		}
	}
	if err := json.Unmarshal([]byte(result), &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.Auth.ID != "existing-id" || !refreshed.Auth.Disabled || string(refreshed.Auth.StorageJSON) != string(storage) {
		t.Fatalf("unexpected refreshed auth: %#v", refreshed)
	}
}
