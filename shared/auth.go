package shared

import (
	"encoding/json"
	"time"
)

// DecodeStorage unmarshals a raw auth blob into a storage struct.
// The caller provides a pointer to their own storage type.
func DecodeStorage(raw []byte, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// ExpiryFromNow returns a time in the future given seconds from now.
func ExpiryFromNow(expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second)
}

// FirstNonEmpty returns the first non-empty string from vals.
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
