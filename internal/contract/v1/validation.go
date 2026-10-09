package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The standard JSON decoder substitutes malformed Unicode. Reject it before decoding.
func validUnicodeJSON(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	inString := false
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		v, ok := hex4(b, i+1)
		if !ok {
			return false
		}
		i += 4
		if v >= 0xDC00 && v <= 0xDFFF {
			return false
		}
		if v >= 0xD800 && v <= 0xDBFF {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			lo, ok := hex4(b, i+3)
			if !ok || lo < 0xDC00 || lo > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}
func hex4(b []byte, start int) (uint16, bool) {
	if start+4 > len(b) {
		return 0, false
	}
	var v uint16
	for _, c := range b[start : start+4] {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			v += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			v += uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return v, true
}

// ValidateOperationPayload distinguishes absent fields from explicit empty/zero
// values. Shared DTOs never expand the fields allowed by an individual command.
func ValidateOperationPayload(r OperationRequest) error {
	if !validUnicodeJSON(r.Payload) || walkJSON(r.Payload) != nil {
		return errors.New("invalid payload")
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(r.Payload, &m) != nil || m == nil {
		return errors.New("object required")
	}
	allowed := ""
	switch r.Operation {
	case "app.register":
		allowed = "app_id platform provider_app_id name"
	case "shop.register":
		allowed = "external_shop_id platform provider_shop_id name"
	case "brand.register":
		allowed = "brand_id name"
	case "internal_shop.register":
		allowed = "internal_shop_id name"
	case "app.list", "shop.list", "brand.list", "internal_shop.list":
		allowed = "offset limit"
	case "installation.list", "credential.status":
		allowed = "app_id external_shop_id offset limit"
	case "shop.map_brand":
		allowed = "external_shop_id brand_id expected_revision"
	case "shop.unmap_brand":
		allowed = "external_shop_id expected_revision"
	case "brand.map_internal_shop":
		allowed = "brand_id internal_shop_id expected_revision"
	case "brand.unmap_internal_shop":
		allowed = "brand_id expected_revision"
	case "installation.create":
		allowed = "app_id external_shop_id expected_revision"
	case "credential.import":
		allowed = "scope app_id external_shop_id type value replace expected_generation expiry_known expires_at confirm"
	case "credential.confirm", "credential.restore":
		allowed = "scope app_id external_shop_id type expected_generation"
	case "credential.mark_invalid":
		allowed = "scope app_id external_shop_id type expected_generation reason"
	case "credential.disable":
		allowed = "scope app_id external_shop_id type"
	case "credential.query":
		allowed = "by app_id external_shop_id brand_id internal_shop_id"
	case "audit.list":
		allowed = "from to operation target result operation_id offset limit"
	case "recovery.open":
		allowed = "expected_revision accounts_verified ownership_verified values_verified credentials"
		var in RecoveryOpenInput
		if DecodeStrict(bytes.NewReader(r.Payload), &in) != nil || RequireFields(r.Payload, "expected_revision", "accounts_verified", "ownership_verified", "values_verified", "credentials") != nil || in.ExpectedRevision <= 0 || !in.AccountsVerified || !in.OwnershipVerified || !in.ValuesVerified || in.Credentials == nil {
			return errors.New("invalid recovery review")
		}
		var refs []json.RawMessage
		if json.Unmarshal(m["credentials"], &refs) != nil {
			return errors.New("invalid credentials")
		}
		for _, ref := range refs {
			if ValidateOperationPayload(OperationRequest{Operation: "credential.confirm", Payload: ref}) != nil || RequireFields(ref, "scope", "app_id", "type", "expected_generation") != nil {
				return errors.New("invalid recovery reference")
			}
		}
	case "health", "operations.backup", "operations.retention", "recovery.status":
		allowed = ""
	default:
		return errors.New("unknown operation")
	}
	set := map[string]bool{}
	for _, k := range strings.Fields(allowed) {
		set[k] = true
	}
	for k, v := range m {
		if !set[k] {
			return errors.New("field not allowed")
		}
		if strings.HasSuffix(k, "_id") && k != "operation_id" {
			var s string
			if json.Unmarshal(v, &s) != nil || !ValidIdentifier(s) {
				return errors.New("invalid identifier")
			}
		}
		switch k {
		case "expected_revision", "expected_generation", "limit":
			var n int64
			if json.Unmarshal(v, &n) != nil || n <= 0 || k == "limit" && n > 1000 {
				return errors.New("invalid positive integer")
			}
		case "offset":
			var n int64
			if json.Unmarshal(v, &n) != nil || n < 0 {
				return errors.New("invalid offset")
			}
		}
	}
	scope := stringField(m, "scope")
	if strings.HasPrefix(r.Operation, "credential.") && r.Operation != "credential.query" && r.Operation != "credential.status" {
		if scope == "app" {
			if _, ok := m["external_shop_id"]; ok {
				return errors.New("shop field forbidden")
			}
		} else if scope == "shop" {
			if _, ok := m["external_shop_id"]; !ok {
				return errors.New("shop field required")
			}
		}
	}
	if r.Operation == "credential.import" {
		var replace bool
		json.Unmarshal(m["replace"], &replace)
		_, expected := m["expected_generation"]
		if replace != expected {
			return errors.New("replacement expectation mismatch")
		}
	}
	if r.Operation == "credential.query" {
		switch stringField(m, "by") {
		case "external_shop":
			if _, ok := m["brand_id"]; ok {
				return errors.New("brand selector forbidden")
			}
			if _, ok := m["internal_shop_id"]; ok {
				return errors.New("internal selector forbidden")
			}
		case "brand":
			if _, ok := m["internal_shop_id"]; ok {
				return errors.New("internal selector forbidden")
			}
		case "internal_shop":
			if _, ok := m["brand_id"]; ok {
				return errors.New("brand selector forbidden")
			}
		}
	}
	return nil
}
func stringField(m map[string]json.RawMessage, k string) string {
	var s string
	json.Unmarshal(m[k], &s)
	return s
}
func ValidIdentifier(s string) bool {
	if len(s) == 0 || len(s) > 128 || strings.Contains(s, "|") || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsSpace(c) || unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func validPayloadObject(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) > 0 && validUnicodeJSON(raw)
}
