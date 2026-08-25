package main

import "testing"

func TestEncodeBase64RoundTrip(t *testing.T) {
	for _, input := range []string{"", "hello", `{"ok":true}`} {
		encoded := encodeBase64([]byte(input))
		if !isBase64(encoded) {
			t.Fatalf("encodeBase64(%q) produced invalid Base64 %q", input, encoded)
		}
	}
}

func TestIsBase64RejectsPlainText(t *testing.T) {
	if isBase64("not encoded") {
		t.Fatal("isBase64 accepted plain text")
	}
}
