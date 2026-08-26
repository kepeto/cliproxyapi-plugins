package shared

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// JWTExpiry extracts the exp claim as a scheduling hint. It does not validate
// the token signature and must never be used as an authentication decision.
func JWTExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp json.RawMessage `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || len(claims.Exp) == 0 {
		return time.Time{}, false
	}
	var seconds float64
	if json.Unmarshal(claims.Exp, &seconds) != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(seconds), 0), true
}
