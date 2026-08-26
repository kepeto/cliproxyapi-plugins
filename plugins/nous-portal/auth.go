package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

var (
	loginCount int
	loginMu    sync.Mutex
)

// nextAccountFileName returns the first unused account filename. The auth
// directory scan prevents a plugin reload from overwriting an existing file;
// the mutex prevents concurrent login.start calls from selecting the same name.
func nextAccountFileName(raw []byte) string {
	var req struct {
		Host struct {
			AuthDir string
		}
	}
	_ = json.Unmarshal(raw, &req)
	loginMu.Lock()
	defer loginMu.Unlock()
	for {
		loginCount++
		name := ProviderID + ".json"
		if loginCount > 1 {
			name = fmt.Sprintf("%s-%d.json", ProviderID, loginCount)
		}
		if req.Host.AuthDir == "" {
			return name
		}
		if _, err := os.Stat(filepath.Join(req.Host.AuthDir, name)); os.IsNotExist(err) {
			return name
		} else if err != nil {
			return name
		}
	}
}

func generateAccountID() string {
	return ProviderID + "-" + randomState()
}

// handleAuthParse recognizes nous-portal credential JSON files and returns the auth record.
func handleAuthParse(raw []byte) ([]byte, error) {
	storage := raw
	fileName := ""
	var envelope struct {
		FileName string `json:"FileName"`
		RawJSON  []byte `json:"RawJSON"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.RawJSON) > 0 {
		storage = envelope.RawJSON
		fileName = strings.TrimSpace(envelope.FileName)
	}
	var probe map[string]any
	if err := json.Unmarshal(storage, &probe); err != nil {
		return okEnvelopeJSON(`{"Handled":false}`)
	}
	typ, _ := probe["type"].(string)
	if strings.ToLower(strings.TrimSpace(typ)) != ProviderID {
		return okEnvelopeJSON(`{"Handled":false}`)
	}
	store := decodeStorage(storage)
	if !store.structuralValid() {
		return okEnvelopeJSON(`{"Handled":false}`)
	}
	if fileName == "" {
		fileName = strings.TrimSpace(store.FileName)
	}
	if fileName == "" {
		fileName = "nous-portal.json"
	}
	store.FileName = fileName
	label := "Nous Portal"
	id := store.AccountID
	stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if id == "" && stem != "" && strings.ToLower(stem) != ProviderID {
		label += " (" + stem + ")"
		id = stem
	}
	if id == "" && store.RefreshToken != "" {
		h := sha256.Sum256([]byte(store.RefreshToken))
		id = ProviderID + "-" + hex.EncodeToString(h[:8])
	}
	if id == "" {
		id = ProviderID
	}

	auth := buildAuthDataWithID(store, ProviderID, fileName, label, id, nil)
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
		return errorEnvelopeWithStatus("login_start_failed", "device code request returned "+strconv.Itoa(status), status), nil
	}
	var dc deviceCodeResponse
	if err := json.Unmarshal(body, &dc); err != nil {
		return errorEnvelope("login_start_failed", "invalid device code response: "+err.Error()), nil
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURIComplete == "" || dc.ExpiresIn == 0 || dc.Interval == 0 {
		return errorEnvelope("login_start_failed", "device code response missing required fields"), nil
	}

	state := randomState()
	accountFileName := nextAccountFileName(raw)
	loginStates.put(state, &loginState{
		deviceCode:               dc.DeviceCode,
		verificationURI:          dc.VerificationURIComplete,
		userCode:                 dc.UserCode,
		expiresAt:                time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second),
		interval:                 dc.Interval,
		portalBaseURL:            portal,
		inferenceBaseURLExplicit: strings.TrimSpace(cfg.InferenceBaseURL) != "",
		clientID:                 cfg.clientID(),
		scope:                    cfg.scope(),
		createdAt:                time.Now(),
		accountFileName:          accountFileName,
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
		inferenceURL := ""
		if ls.inferenceBaseURLExplicit {
			inferenceURL = trimHTTP(ls.inferenceBaseURL)
		}
		if inferenceURL == "" {
			inferenceURL = trimHTTP(tok.InferenceBaseURL)
		}
		if inferenceURL == "" {
			inferenceURL = ls.inferenceBaseURL
		}
		fileName := ls.accountFileName
		if fileName == "" {
			fileName = "nous-portal.json"
		}
		store := storageJSON{
			AccessToken:      tok.AccessToken,
			RefreshToken:     tok.RefreshToken,
			ExpiresAt:        expiryFromToken(tok.AccessToken, tok.ExpiresIn),
			PortalBaseURL:    ls.portalBaseURL,
			InferenceBaseURL: inferenceURL,
			ClientID:         ls.clientID,
			Scope:            ls.scope,
			AccountID:        generateAccountID(),
			FileName:         fileName,
		}
		loginStates.delete(req.State)
		auth := buildAuthDataWithID(store, ProviderID, fileName, "Nous Portal", store.AccountID, nil)
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
	if store.RefreshToken == "" {
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
		return errorEnvelopeWithStatus("refresh_failed", "refresh returned "+strconv.Itoa(status), status), nil
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return errorEnvelope("refresh_failed", "invalid refresh response: "+err.Error()), nil
	}
	if tok.AccessToken == "" {
		return errorEnvelope("refresh_failed", "refresh response missing access_token"), nil
	}
	inferenceURL := trimHTTP(store.InferenceBaseURL)
	if inferenceURL == "" {
		inferenceURL = trimHTTP(tok.InferenceBaseURL)
	}
	if inferenceURL == "" {
		return errorEnvelope("refresh_failed", "refresh response missing inference_base_url"), nil
	}
	next := storageJSON{
		AccessToken:      tok.AccessToken,
		RefreshToken:     firstNonEmpty(tok.RefreshToken, store.RefreshToken),
		ExpiresAt:        expiryFromToken(tok.AccessToken, tok.ExpiresIn),
		PortalBaseURL:    portal,
		InferenceBaseURL: inferenceURL,
		ClientID:         clientID,
		Scope:            firstNonEmpty(tok.Scope, store.Scope),
		AccountID:        store.AccountID,
		FileName:         store.FileName,
		ModelCatalog:     store.ModelCatalog,
	}
	storageJSON, _ := json.Marshal(next)
	auth := map[string]any{"Provider": ProviderID, "StorageJSON": storageJSON}
	return okEnvelopeJSON(mustJSON(map[string]any{
		"Auth":             auth,
		"NextRefreshAfter": nextRefreshAfter(next.ExpiresAt),
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

const refreshSkew = 5 * time.Minute

func expiryFromNow(expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second)
}

func expiryFromToken(token string, expiresIn int) time.Time {
	if expiry, ok := shared.JWTExpiry(token); ok {
		return expiry
	}
	return expiryFromNow(expiresIn)
}

func nextRefreshAfter(expiresAt time.Time) string {
	if expiresAt.IsZero() {
		return ""
	}
	at := expiresAt.Add(-refreshSkew)
	if at.Before(time.Now()) {
		at = time.Now()
	}
	return at.Format(time.RFC3339)
}

func buildAuthData(store storageJSON, provider, fileName, label string, extraMeta map[string]any) map[string]any {
	meta := map[string]any{
		"type":            provider,
		"username":        "nous-portal",
		"portal_base_url": store.PortalBaseURL,
		"logo":            "https://hermes-agent.nousresearch.com/favicon.ico",
		"icon":            "https://hermes-agent.nousresearch.com/favicon.ico",
		"description":     "Nous Portal OAuth",
		"homepage":        "https://portal.nousresearch.com",
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
