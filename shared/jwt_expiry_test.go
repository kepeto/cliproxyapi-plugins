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

func TestJWTClaim(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"user@example.test","member_spend_usd":1.5,"empty":"","n":null}`))
	token := fmt.Sprintf("header.%s.signature", payload)
	if got, ok := JWTClaim(token, "email"); !ok || got != "user@example.test" {
		t.Fatalf("JWTClaim email = %q, %v", got, ok)
	}
	if got, ok := JWTClaim(token, "member_spend_usd"); !ok || got != "1.5" {
		t.Fatalf("JWTClaim spend = %q, %v", got, ok)
	}
	for _, missing := range []string{"absent", "empty", "n"} {
		if _, ok := JWTClaim(token, missing); ok {
			t.Fatalf("JWTClaim accepted %q", missing)
		}
	}
	if _, ok := JWTClaim("opaque", "email"); ok {
		t.Fatal("JWTClaim accepted opaque token")
	}
}
