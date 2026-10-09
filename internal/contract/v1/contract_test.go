package v1

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDecodeStrict(t *testing.T) {
	tests := []string{"", `null`, `[]`, `{} {}`, `{"version":"contract/v1","operation":"x","payload":{},"payload":{}}`, `{"version":"contract/v1","operation":"x","payload":{},"extra":1}`, `{"Version":"contract/v1","operation":"x","payload":{}}`, `{"version":null,"operation":"x","payload":{}}`, `{"version":"contract/v1","operation":"x","payload":{"a":1,"a":2}}`, `{"version":"contract/v1","operation":"x","payload":{"a":[{"b":1,"b":2}]}}`, `{"version":"contract/v1","operation":3,"payload":{}}`, strings.Repeat(" ", MaxRequestBytes+1)}
	for _, s := range tests {
		var x OperationRequest
		if err := DecodeStrict(strings.NewReader(s), &x); err == nil {
			t.Fatalf("accepted invalid input %.100s", s)
		} else if strings.Contains(err.Error(), "contract/v1") {
			t.Fatal("input echoed")
		}
	}
	var x OperationRequest
	if err := DecodeStrict(strings.NewReader(`{"version":"contract/v1","operation":"health","payload":{}}`), &x); err != nil {
		t.Fatal(err)
	}
	if ValidateOperationRequest(x) != nil {
		t.Fatal("valid request rejected")
	}
	for _, r := range []OperationRequest{{Version: Version, Operation: "unknown", Payload: []byte(`{}`)}, {Version: Version, Operation: "health", Payload: []byte(`null`)}, {Version: "v2", Operation: "health", Payload: []byte(`{}`)}} {
		if ValidateOperationRequest(r) == nil {
			t.Fatal("invalid request accepted")
		}
	}
}
func TestPayloadFieldsTypesAndErrors(t *testing.T) {
	var i ImportCredentialInput
	for _, s := range []string{`{"type":null}`, `{"type":1}`, `{"confirm":null}`, `{"expires_at":[]}`} {
		if DecodeStrict(strings.NewReader(s), &i) == nil {
			t.Fatal(s)
		}
	}
	if DecodeStrict(strings.NewReader(`{"expires_at":null,"confirm":false}`), &i) != nil {
		t.Fatal("nullable expiry")
	}
	if RequireFields(json.RawMessage(`{}`), "type") == nil {
		t.Fatal("missing field")
	}
	if RequireFields(json.RawMessage(`null`), "type") == nil {
		t.Fatal("null")
	}
	if RequireFields(json.RawMessage(`{"type":"x"}`), "type") != nil {
		t.Fatal("present")
	}
	if SafeError(errors.New("fixture-secret")).Error != ErrInternal {
		t.Fatal("dependency mapping")
	}
	for _, code := range []ErrorCode{ErrInputInvalid, ErrNotFound, ErrSelectionRequired, ErrMappingIncomplete, ErrMappingMismatch, ErrPermissionDenied, ErrCredentialMissing, ErrCredentialExpired, ErrCredentialDisabled, ErrInternal, ErrorCode("unknown")} {
		f := Fail(code)
		if f.Error() == "" || SafeError(f).Message == "" {
			t.Fatal(code)
		}
	}
	if DecodeStrict(strings.NewReader(`{}`), nil) == nil {
		t.Fatal("nil destination")
	}
}
