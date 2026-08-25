package shared

import (
	"errors"
	"testing"
	"time"
)

func TestModelHealthQuarantineRecovery(t *testing.T) {
	health := NewModelHealth(3, time.Hour)
	for i := 0; i < 2; i++ {
		if health.RecordFailure("provider", "model") {
			t.Fatal("model quarantined too early")
		}
		if !health.Allow("provider", "model") {
			t.Fatal("model unavailable before threshold")
		}
	}
	if !health.RecordFailure("provider", "model") || health.Allow("provider", "model") {
		t.Fatal("third failure did not quarantine model")
	}
	health.RecordSuccess("provider", "model")
	if !health.Allow("provider", "model") || health.Hidden("provider", "model") {
		t.Fatal("successful recovery did not clear quarantine")
	}
}

func TestModelHealthFiltersAliasesByCanonicalID(t *testing.T) {
	health := NewModelHealth(1, time.Hour)
	health.RecordFailure("provider", "canonical")
	models := health.Filter("provider", []string{"canonical", "alias"})
	if len(models) != 1 || models[0] != "alias" {
		t.Fatalf("unexpected filtered models: %#v", models)
	}
}

func TestIsModelSpecificFailure(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"transport", 0, "", true},
		{"unavailable", 400, `{"error":"model unavailable"}`, true},
		{"auth", 401, `{"error":"model unavailable"}`, false},
		{"rate limit", 429, "model unavailable", false},
		{"generic server", 500, "model unavailable", false},
		{"caller error", 400, "bad request", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.name == "transport" {
				err = errors.New("timeout")
			}
			if got := IsModelSpecificFailure(tc.status, []byte(tc.body), err); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidChatResponse(t *testing.T) {
	if !ValidChatResponse([]byte(`{"choices":[{"message":{}}]}`)) {
		t.Fatal("valid response rejected")
	}
	if ValidChatResponse([]byte(`{"choices":[]}`)) {
		t.Fatal("empty choices accepted")
	}
	if ValidChatResponse([]byte(`{"error":{"message":"failed"}}`)) {
		t.Fatal("error response accepted")
	}
}
