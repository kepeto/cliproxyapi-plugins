package main

import (
	"sync"
	"time"

	"github.com/kepeto/cliproxyapi-plugins/shared"
)

// ProviderID is the canonical provider key. It must match the lowercase identifier
// returned by auth.identifier / executor.identifier and the config key.
const ProviderID = "nous-portal"

// Default endpoints mirror kepeto/pi-nous-portal-provider.
const (
	defaultPortalBaseURL    = "https://portal.nousresearch.com"
	defaultInferenceBaseURL = "https://inference-api.nousresearch.com/v1"
	defaultClientID         = "hermes-cli"
	defaultScope            = "inference:invoke"
	defaultRequestTimeout   = 15 * time.Second
)

// deviceCodeResponse is the OAuth 2.0 device authorization response.
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// tokenResponse is the OAuth 2.0 token endpoint response.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	InferenceBaseURL string `json:"inference_base_url"`
}

// loginState is kept between auth.login.start and auth.login.poll.
type loginState struct {
	deviceCode               string
	verificationURI          string
	userCode                 string
	expiresAt                time.Time
	interval                 int
	portalBaseURL            string
	inferenceBaseURL         string
	inferenceBaseURLExplicit bool
	clientID                 string
	scope                    string
	createdAt                time.Time
	accountFileName          string // unique filename for multi-account support
}

// authStateStore holds in-progress login flows keyed by opaque state token.
type authStateStore struct {
	mu     sync.Mutex
	states map[string]*loginState
}

var loginStates = &authStateStore{states: make(map[string]*loginState)}

func (s *authStateStore) put(state string, ls *loginState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state] = ls
}

func (s *authStateStore) get(state string) (*loginState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ls, ok := s.states[state]
	return ls, ok
}

func (s *authStateStore) delete(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, state)
}

func (s *authStateStore) purgeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.states {
		if now.After(v.expiresAt) {
			delete(s.states, k)
		}
	}
}

// shutdownLoginPollers is invoked on plugin shutdown; it drops pending flows.
func shutdownLoginPollers() {
	loginStates.mu.Lock()
	loginStates.states = make(map[string]*loginState)
	loginStates.mu.Unlock()
}

// modelAliases maps client-visible alias IDs to upstream IDs (plugin config).
var modelAliases = shared.NewAliasTable()
var modelHealth = shared.NewModelHealth(3, 15*time.Minute)

// config holds plugin-level overrides resolved from plugins.configs.nous-portal.
type config struct {
	PortalBaseURL    string            `json:"portal_base_url"`
	InferenceBaseURL string            `json:"inference_base_url"`
	ClientID         string            `json:"client_id"`
	Scope            string            `json:"scope"`
	Prefix           string            `json:"prefix"`
	ModelAliases     map[string]string `json:"model_aliases"`
}

func (c config) portalBaseURL() string {
	if v := trimHTTP(c.PortalBaseURL); v != "" {
		return v
	}
	return defaultPortalBaseURL
}

func (c config) inferenceBaseURL() string {
	if v := trimHTTP(c.InferenceBaseURL); v != "" {
		return v
	}
	return defaultInferenceBaseURL
}

func (c config) clientID() string {
	if v := trimHTTP(c.ClientID); v != "" {
		return v
	}
	return defaultClientID
}

func (c config) scope() string {
	if v := trimHTTP(c.Scope); v != "" {
		return v
	}
	return defaultScope
}

func (c config) prefix() string {
	if v := trimHTTP(c.Prefix); v != "" {
		return v
	}
	return ""
}

// resolveConfig decodes the plugin config YAML subtree forwarded by the host.
// applyHostAliases merges dashboard-managed oauth-model-alias entries relayed by
// the host inside auth.* request payloads. No-op when none are present.
func applyHostAliases(raw []byte) {
	if host, ok := shared.HostModelAliases(raw, ProviderID); ok {
		modelAliases.SetHost(host)
	}
}

// applyConfig applies the host-forwarded config subtree (prefix, aliases).
func applyConfig(raw []byte) {
	cfg := resolveConfig(shared.ConfigBytesFromLifecycle(raw))
	setPluginPrefix(cfg.prefix())
	modelAliases.SetConfig(cfg.ModelAliases)
}

func resolveConfig(raw []byte) config {
	cfg := config{}
	// Host forwards the config subtree as YAML bytes; tolerate raw JSON too.
	_ = shared.UnmarshalConfig(raw, &cfg)
	return cfg
}

// storageJSON is the persisted auth blob stored by the host under the "nous-portal" type.
func nousHealthScope(store storageJSON) string {
	account := store.AccountID
	if account == "" {
		account = "default"
	}
	return ProviderID + "|" + shared.TrimHTTP(store.InferenceBaseURL) + "|" + account
}

type storageJSON struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at,omitzero"`
	PortalBaseURL    string    `json:"portal_base_url"`
	InferenceBaseURL string    `json:"inference_base_url"`
	ClientID         string    `json:"client_id"`
	Scope            string    `json:"scope"`
	AccountID        string    `json:"account_id,omitempty"`
	FileName         string    `json:"file_name,omitempty"`
	ModelCatalog     []byte    `json:"model_catalog,omitempty"`
}

func (s storageJSON) structuralValid() bool {
	return s.AccessToken != "" && s.InferenceBaseURL != ""
}

func (s storageJSON) accessTokenUsable() bool {
	return s.structuralValid() && (s.ExpiresAt.IsZero() || time.Now().Before(s.ExpiresAt))
}

// valid remains structural for compatibility: CPA must see expired auth and
// receive the upstream 401 path that triggers auth.refresh.
func (s storageJSON) valid() bool {
	return s.structuralValid()
}
