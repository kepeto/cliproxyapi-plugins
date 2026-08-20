package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// loginCount tracks OAuth logins within this plugin instance for multi-account
// filename generation. Resets on plugin reload; the user can manually rename
// files if a stable primary filename is required.
var loginCount int

// nextAccountFileName returns the next available auth filename for a new
// nous-portal-free account. The first login in a session writes nous-portal-free.json;
// subsequent logins write nous-portal-free-2.json, nous-portal-free-3.json, etc.
// Each file becomes an independent client in CPA with its own OAuth tokens.
func nextAccountFileName() string {
	loginCount++
	if loginCount == 1 {
		return "nous-portal-free.json"
	}
	return fmt.Sprintf("nous-portal-free-%d.json", loginCount)
}

// handleAuthParse recognizes nous-portal-free credential JSON files and returns the auth record.
// It only accepts standard storageJSON with "type": "nous-portal-free".
// Auth files from nous-portal are NOT shared; each plugin maintains separate credentials.
func handleAuthParse(raw []byte) ([]byte, error) {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return okEnvelopeJSON(`{"Handled":false}`)
	}
	typ, _ := probe["type"].(string)
	if strings.ToLower(strings.TrimSpace(typ)) != ProviderID {
		return okEnvelopeJSON(`{"Handled":false}`)
	}
	store := decodeStorage(raw)
	if !store.valid() {
		return okEnvelopeJSON(`{"Handled":false}`)
	}
	fileName := "nous-portal-free.json"
	label := "Nous Portal Free"
	id := ProviderID

	// Prefer explicit AccountID so each credential has a stable unique identity.
	if store.AccountID != "" {
		id = store.AccountID
	} else if fname, ok := probe["FileName"].(string); ok && strings.TrimSpace(fname) != "" {
		fileName = strings.TrimSpace(fname)
		stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		if stem != "" && strings.ToLower(stem) != ProviderID {
			label = label + " (" + stem + ")"
			id = stem
		}
	} else if store.RefreshToken != "" {
		// Legacy files without AccountID/FileName: derive a deterministic ID
		// from the refresh token so multiple files don't collapse to one ID.
		h := sha256.Sum256([]byte(store.RefreshToken))
		id = ProviderID + "-" + hex.EncodeToString(h[:4])
	}

	auth := buildAuthDataWithID(store, "nous-portal-free", fileName, label, id, nil)
	return okEnvelopeJSON(mustJSON(map[string]any{
		"Handled": true,
		"Auth":    auth,
	}))
}

// handleAuthLoginStart begins a device-code OAuth flow.
func handleAuthLoginStart(raw []byte) ([]byte, error) {
	cfg := resolveConfig(raw)
	portal := cfg.portalBaseURL()

	status, body, err := httpPostForm(portal, "/api/oauth/device/code",
		map[string]string{
			"client_id": cfg.clientID(),
			"scope":     cfg.scope(),
		}, defaultRequestTimeout)
	if err != nil {
		return errorEnvelope("login_start_failed", "device code request failed: "+err.Error()), nil
	}
	if status != 200 {
		return errorEnvelopeWithStatus("login_start_failed", "device code request returned "+itoa(status), status), nil
	}
	var dc deviceCodeResponse
	if err := json.Unmarshal(body, &dc); err != nil {
		return errorEnvelope("login_start_failed", "invalid device code response: "+err.Error()), nil
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURIComplete == "" || dc.ExpiresIn == 0 || dc.Interval == 0 {
		return errorEnvelope("login_start_failed", "device code response missing required fields"), nil
	}

	state := randomState()
	accountFileName := nextAccountFileName()
	loginStates.put(state, &loginState{
		deviceCode:       dc.DeviceCode,
		verificationURI:  dc.VerificationURIComplete,
		userCode:         dc.UserCode,
		expiresAt:        time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second),
		interval:         dc.Interval,
		portalBaseURL:    portal,
		inferenceBaseURL: cfg.inferenceBaseURL(),
		clientID:         cfg.clientID(),
		scope:            cfg.scope(),
		createdAt:        time.Now(),
		accountFileName:  accountFileName,
	})

	return okEnvelopeJSON(mustJSON(map[string]any{
		"Provider":  ProviderID,
		"URL":       dc.VerificationURIComplete,
		"State":     state,
		"ExpiresAt": time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second).Format(time.RFC3339),
		"Metadata": map[string]any{
			"user_code": dc.UserCode,
			"interval":  dc.Interval,
		},
	}))
}

// handleAuthLoginPoll polls the token endpoint until the user authorizes.
func handleAuthLoginPoll(raw []byte) ([]byte, error) {
	var req struct {
		State    string         `json:"State"`
		Metadata map[string]any `json:"Metadata"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorEnvelope("login_poll_failed", "invalid poll request: "+err.Error()), nil
	}
	ls, ok := loginStates.get(req.State)
	if !ok {
		return errorEnvelope("login_poll_failed", "login state not found or expired"), nil
	}
	if time.Now().After(ls.expiresAt) {
		loginStates.delete(req.State)
		return okEnvelopeJSON(mustJSON(map[string]any{
			"Status":  "error",
			"Message": "device authorization expired",
		}))
	}

	// Respect host-provided interval overrides when supplied.
	interval := ls.interval
	if m := req.Metadata; m != nil {
		if v, ok := m["interval"].(float64); ok && v > 0 {
			interval = int(v)
		}
	}

	status, body, err := httpPostForm(ls.portalBaseURL, "/api/oauth/token",
		map[string]string{
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
			"client_id":   ls.clientID,
			"device_code": ls.deviceCode,
		}, defaultRequestTimeout)
	if err != nil {
		return errorEnvelope("login_poll_failed", "token poll failed: "+err.Error()), nil
	}

	if status == 200 {
		var tok tokenResponse
		if err := json.Unmarshal(body, &tok); err != nil {
			return errorEnvelope("login_poll_failed", "invalid token response: "+err.Error()), nil
		}
		if tok.AccessToken == "" || tok.RefreshToken == "" {
			return errorEnvelope("login_poll_failed", "token response missing access/refresh token"), nil
		}
		inferenceURL := trimHTTP(tok.InferenceBaseURL)
		if inferenceURL == "" {
			inferenceURL = ls.inferenceBaseURL
		}
		store := storageJSON{
			AccessToken:      tok.AccessToken,
			RefreshToken:     tok.RefreshToken,
			ExpiresAt:        expiryFromNow(tok.ExpiresIn),
			PortalBaseURL:    ls.portalBaseURL,
			InferenceBaseURL: inferenceURL,
			ClientID:         ls.clientID,
			Scope:            ls.scope,
			AccountID:        generateAccountID(),
		}
		loginStates.delete(req.State)
		fileName := ls.accountFileName
		if fileName == "" {
			fileName = "nous-portal-free.json"
		}
		auth := buildAuthData(store, "nous-portal-free", fileName, "Nous Portal Free", nil)
		return okEnvelopeJSON(mustJSON(map[string]any{
			"Status":  "success",
			"Message": "Nous Portal login complete",
			"Auth":    auth,
		}))
	}

	// Inspect the OAuth error code to decide whether to keep polling.
	var oerr struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &oerr)
	switch oerr.Error {
	case "authorization_pending":
		return okEnvelopeJSON(mustJSON(map[string]any{
			"Status":   "pending",
			"Message":  "waiting for authorization",
			"Metadata": map[string]any{"interval": interval},
		}))
	case "slow_down":
		return okEnvelopeJSON(mustJSON(map[string]any{
			"Status":   "pending",
			"Message":  "slow down",
			"Metadata": map[string]any{"interval": interval + 1},
		}))
	case "access_denied", "authorization_denied":
		loginStates.delete(req.State)
		return okEnvelopeJSON(mustJSON(map[string]any{
			"Status":  "error",
			"Message": "login denied",
		}))
	case "expired_token":
		loginStates.delete(req.State)
		return okEnvelopeJSON(mustJSON(map[string]any{
			"Status":  "error",
			"Message": "device authorization expired",
		}))
	default:
		return okEnvelopeJSON(mustJSON(map[string]any{
			"Status":   "pending",
			"Message":  oerr.ErrorDescription,
			"Metadata": map[string]any{"interval": interval},
		}))
	}
}

// handleAuthRefresh rotates the access token using the stored refresh token.
func handleAuthRefresh(raw []byte) ([]byte, error) {
	var req struct {
		AuthID      string `json:"AuthID"`
		StorageJSON []byte `json:"StorageJSON"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorEnvelope("refresh_failed", "invalid refresh request: "+err.Error()), nil
	}
	store := decodeStorage(req.StorageJSON)
	if !store.valid() || store.RefreshToken == "" {
		return errorEnvelope("refresh_failed", "no usable refresh token"), nil
	}

	portal := store.PortalBaseURL
	if portal == "" {
		portal = defaultPortalBaseURL
	}
	clientID := store.ClientID
	if clientID == "" {
		clientID = defaultClientID
	}

	status, body, err := httpPostForm(portal, "/api/oauth/token",
		map[string]string{
			"grant_type":    "refresh_token",
			"client_id":     clientID,
			"refresh_token": store.RefreshToken,
		}, defaultRequestTimeout)
	if err != nil {
		return errorEnvelope("refresh_failed", "refresh request failed: "+err.Error()), nil
	}
	if status != 200 {
		return errorEnvelopeWithStatus("refresh_failed", "refresh returned "+itoa(status), status), nil
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return errorEnvelope("refresh_failed", "invalid refresh response: "+err.Error()), nil
	}
	if tok.AccessToken == "" {
		return errorEnvelope("refresh_failed", "refresh response missing access_token"), nil
	}
	inferenceURL := trimHTTP(tok.InferenceBaseURL)
	if inferenceURL == "" {
		inferenceURL = store.InferenceBaseURL
	}
	next := storageJSON{
		AccessToken:      tok.AccessToken,
		RefreshToken:     firstNonEmpty(tok.RefreshToken, store.RefreshToken),
		ExpiresAt:        expiryFromNow(tok.ExpiresIn),
		PortalBaseURL:    portal,
		InferenceBaseURL: inferenceURL,
		ClientID:         clientID,
		Scope:            firstNonEmpty(tok.Scope, store.Scope),
		AccountID:        store.AccountID,
	}
	id := req.AuthID
	if id == "" {
		id = "nous-portal-free"
		if store.AccountID != "" {
			id = store.AccountID
		} else if store.RefreshToken != "" {
			h := sha256.Sum256([]byte(store.RefreshToken))
			id = "nous-portal-free-" + hex.EncodeToString(h[:4])
		}
	}
	auth := buildAuthDataWithID(next, "nous-portal-free", "nous-portal-free.json", "Nous Portal Free", id, nil)
	return okEnvelopeJSON(mustJSON(map[string]any{
		"Auth":             auth,
		"NextRefreshAfter": next.ExpiresAt.Add(-5 * time.Minute).Format(time.RFC3339),
	}))
}

// --- helpers ---

func decodeStorage(raw []byte) storageJSON {
	var s storageJSON
	if len(raw) == 0 {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}

func expiryFromNow(expiresIn int) time.Time {
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	// 5-minute skew so refresh happens before expiry.
	return time.Now().Add(time.Duration(expiresIn)*time.Second - 5*time.Minute)
}

func buildAuthData(store storageJSON, provider, fileName, label string, extraMeta map[string]any) map[string]any {
	meta := map[string]any{
		"type":            provider,
		"username":        "nous-portal-free",
		"portal_base_url": store.PortalBaseURL,
	}
	for k, v := range extraMeta {
		meta[k] = v
	}
	storage, _ := json.Marshal(store)
	return map[string]any{
		"Provider":    provider,
		"ID":          provider,
		"FileName":    fileName,
		"Label":       label,
		"StorageJSON": storage,
		"Metadata":    meta,
		"Attributes": map[string]string{
			"source":   "plugin:" + provider,
			"provider": provider,
		},
	}
}

func buildAuthDataWithID(store storageJSON, provider, fileName, label, id string, extraMeta map[string]any) map[string]any {
	meta := map[string]any{
		"type":            provider,
		"username":        provider,
		"portal_base_url": store.PortalBaseURL,
	}
	for k, v := range extraMeta {
		meta[k] = v
	}
	storage, _ := json.Marshal(store)
	return map[string]any{
		"Provider":    provider,
		"ID":          id,
		"FileName":    fileName,
		"Label":       label,
		"StorageJSON": storage,
		"Metadata":    meta,
		"Attributes": map[string]string{
			"source":   "plugin:" + provider,
			"provider": provider,
		},
	}
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// generateAccountID creates a short random identifier for a newly authenticated
// account. Used to keep multiple nous-portal-free credentials distinct in CPA.
func generateAccountID() string {
	var buf [4]byte
	for i := range buf {
		buf[i] = byte('a' + (time.Now().UnixNano() >> (i * 8) & 0xF))
	}
	return ProviderID + "-" + string(buf[:])
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
