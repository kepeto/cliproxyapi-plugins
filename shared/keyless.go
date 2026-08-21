package shared

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KeylessAuth implements the auth lifecycle CPA expects for a keyless provider.
type KeylessAuth struct {
	Provider string
	Label    string
	Token    string
	LoginURL string
}

// Ensure creates the provider's dummy auth file without replacing an existing profile.
func (a KeylessAuth) Ensure(rawReq []byte) error {
	var req struct {
		Host struct {
			AuthDir string
		}
	}
	if err := json.Unmarshal(rawReq, &req); err != nil || strings.TrimSpace(req.Host.AuthDir) == "" {
		return nil
	}

	path := filepath.Join(req.Host.AuthDir, a.Provider+".json")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	file, err := os.CreateTemp(req.Host.AuthDir, "."+a.Provider+"-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(a.storage())
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	if _, err = os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err = os.Link(tempPath, path); errors.Is(err, fs.ErrExist) {
		return nil
	}
	return err
}

// Parse recognizes the provider's auth file inside CPA's AuthParseRequest envelope.
func (a KeylessAuth) Parse(rawReq []byte) string {
	var req struct {
		FileName string
		RawJSON  []byte
	}
	storage := rawReq
	if err := json.Unmarshal(rawReq, &req); err == nil && len(req.RawJSON) > 0 {
		storage = req.RawJSON
	}

	var profile struct {
		Type        string `json:"type"`
		AccessToken string `json:"access_token"`
		Disabled    bool   `json:"disabled"`
	}
	if json.Unmarshal(storage, &profile) != nil || !strings.EqualFold(strings.TrimSpace(profile.Type), a.Provider) {
		return `{"Handled":false}`
	}
	if req.FileName == "" {
		req.FileName = a.Provider + ".json"
	}
	if strings.TrimSpace(profile.AccessToken) == "" {
		profile.AccessToken = a.Token
	}
	return MustJSON(map[string]any{
		"Handled": true,
		"Auth":    a.record(a.Provider, req.FileName, storage, profile.AccessToken, profile.Disabled),
	})
}

// StartLogin returns an immediately-completable keyless login flow.
func (a KeylessAuth) StartLogin(rawReq []byte) (string, error) {
	if err := a.Ensure(rawReq); err != nil {
		return "", err
	}
	return MustJSON(map[string]any{
		"Provider":  a.Provider,
		"URL":       a.LoginURL,
		"State":     a.Provider + "-keyless",
		"ExpiresAt": time.Now().UTC().Add(5 * time.Minute),
	}), nil
}

// PollLogin completes the keyless login without contacting an upstream service.
func (a KeylessAuth) PollLogin(rawReq []byte) (string, error) {
	var req struct {
		State string
	}
	if err := json.Unmarshal(rawReq, &req); err != nil || req.State != a.Provider+"-keyless" {
		return MustJSON(map[string]any{"Status": "error", "Message": "invalid login state"}), nil
	}
	if err := a.Ensure(rawReq); err != nil {
		return "", err
	}
	return MustJSON(map[string]any{
		"Status":  "success",
		"Message": "keyless profile ready",
		"Auth":    a.record(a.Provider, a.Provider+".json", a.storage(), a.Token, false),
	}), nil
}

// Refresh returns the same keyless auth identity and storage.
func (a KeylessAuth) Refresh(rawReq []byte) string {
	var req struct {
		AuthID      string
		StorageJSON []byte
		Metadata    map[string]any
		Attributes  map[string]string
	}
	_ = json.Unmarshal(rawReq, &req)
	if req.AuthID == "" {
		req.AuthID = a.Provider
	}
	if len(req.StorageJSON) == 0 {
		req.StorageJSON = a.storage()
	}

	var profile struct {
		AccessToken string `json:"access_token"`
		Disabled    bool   `json:"disabled"`
	}
	_ = json.Unmarshal(req.StorageJSON, &profile)
	if strings.TrimSpace(profile.AccessToken) == "" {
		profile.AccessToken = a.Token
	}
	auth := a.record(req.AuthID, a.Provider+".json", req.StorageJSON, profile.AccessToken, profile.Disabled)
	if req.Metadata != nil {
		auth["Metadata"] = req.Metadata
	}
	if req.Attributes != nil {
		auth["Attributes"] = req.Attributes
	}
	return MustJSON(map[string]any{"Auth": auth})
}

func (a KeylessAuth) storage() []byte {
	return []byte(MustJSON(map[string]any{
		"type":         a.Provider,
		"access_token": a.Token,
		"disabled":     false,
		"profile_id":   RandomState(),
	}))
}

func (a KeylessAuth) record(id, fileName string, storage []byte, token string, disabled bool) map[string]any {
	return map[string]any{
		"Provider":    a.Provider,
		"ID":          id,
		"FileName":    fileName,
		"Label":       a.Label,
		"Disabled":    disabled,
		"StorageJSON": storage,
		"Metadata": map[string]any{
			"type":         a.Provider,
			"username":     a.Provider,
			"access_token": token,
		},
		"Attributes": map[string]string{
			"source":   "plugin:" + a.Provider,
			"provider": a.Provider,
		},
	}
}
