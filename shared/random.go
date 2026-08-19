package shared

import (
	"crypto/rand"
	"time"
)

// RandomHex returns a hex-encoded random string of n bytes.
func RandomHex(n int) string {
	const chars = "0123456789abcdef"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based pseudo-random
		src := time.Now().UnixNano()
		for i := range b {
			src = (src*6364136223846793005 + 1)
			b[i] = chars[int(src>>56)%16]
		}
		return string(b)
	}
	for i := range b {
		b[i] = chars[b[i]%16]
	}
	return string(b)
}

// RandomState returns a random state token for OAuth flows.
func RandomState() string {
	return RandomHex(16)
}
