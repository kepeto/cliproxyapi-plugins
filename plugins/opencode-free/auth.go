package main

import "github.com/kepeto/cliproxyapi-plugins/shared"

var keylessAuth = shared.KeylessAuth{
	Provider: PROVIDER_ID,
	Label:    "OpenCode Zen Free",
	Token:    "public",
	LoginURL: "https://opencode.ai/",
}

func handleAuthParse(raw []byte) ([]byte, error) {
	return shared.OKEnvelope(keylessAuth.Parse(raw))
}

func handleAuthLoginStart(raw []byte) ([]byte, error) {
	result, err := keylessAuth.StartLogin(raw)
	if err != nil {
		return shared.ErrorEnvelope("auth_bootstrap_failed", err.Error()), nil
	}
	return shared.OKEnvelope(result)
}

func handleAuthLoginPoll(raw []byte) ([]byte, error) {
	result, err := keylessAuth.PollLogin(raw)
	if err != nil {
		return shared.ErrorEnvelope("auth_bootstrap_failed", err.Error()), nil
	}
	return shared.OKEnvelope(result)
}

func handleAuthRefresh(raw []byte) ([]byte, error) {
	return shared.OKEnvelope(keylessAuth.Refresh(raw))
}
