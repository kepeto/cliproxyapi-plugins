package shared

import (
	"encoding/json"
)

// Envelope is the standard plugin response wrapper.
type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *EnvelopeError  `json:"error,omitempty"`
}

// EnvelopeError carries a structured error inside an envelope.
type EnvelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

// OKEnvelope wraps a JSON string in a success envelope.
func OKEnvelope(result string) ([]byte, error) {
	return json.Marshal(Envelope{OK: true, Result: json.RawMessage(result)})
}

// OKEnvelopeRaw wraps pre-serialized JSON in a success envelope.
func OKEnvelopeRaw(result []byte) ([]byte, error) {
	return json.Marshal(Envelope{OK: true, Result: result})
}

// ErrorEnvelope wraps a code+message in a failure envelope.
func ErrorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(Envelope{OK: false, Error: &EnvelopeError{Code: code, Message: message}})
	return raw
}

// ErrorEnvelopeWithStatus wraps a code+message+status in a failure envelope.
func ErrorEnvelopeWithStatus(code, message string, status int) []byte {
	raw, _ := json.Marshal(Envelope{OK: false, Error: &EnvelopeError{Code: code, Message: message, HTTPStatus: status}})
	return raw
}

// MustJSON marshals v to a string, ignoring errors.
func MustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
