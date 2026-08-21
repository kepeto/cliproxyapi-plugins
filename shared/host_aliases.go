package shared

import (
	"encoding/json"
)

// HostModelAliases extracts the host's oauth-model-alias entries for a provider
// from an auth.* request payload (AuthParseRequest / AuthLogin*Request carry
// HostConfigSummary.OAuthModelAlias). Returns (alias -> upstream, true) when
// host info was present, (nil, false) otherwise. The second return lets callers
// distinguish "no aliases configured" from "payload carried no host state".
func HostModelAliases(raw []byte, provider string) (map[string]string, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var req struct {
		Host struct {
			OAuthModelAlias map[string][]struct {
				Name  string
				Alias string
			}
		} `json:"Host"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, false
	}
	if req.Host.OAuthModelAlias == nil {
		return nil, false
	}
	out := make(map[string]string)
	for _, entry := range req.Host.OAuthModelAlias[provider] {
		if entry.Alias != "" && entry.Name != "" {
			out[entry.Alias] = entry.Name
		}
	}
	return out, true
}
