package shared

import (
	"encoding/base64"
	"fmt"
	"testing"
)

func TestJWTExpiry(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":2000000000}`))
	token := fmt.Sprintf("header.%s.signature", payload)
	expiry, ok := JWTExpiry(token)
	if !ok || expiry.Unix() != 2000000000 {
		t.Fatalf("JWTExpiry(%q) = %v, %v", token, expiry, ok)
	}
	for _, invalid := range []string{"opaque", "a.b.c", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":0}`)) + ".c"} {
		if _, ok := JWTExpiry(invalid); ok {
			t.Fatalf("JWTExpiry accepted invalid token %q", invalid)
		}
	}
}
