package main

import (
	"bytes"
	"encoding/json"
	"errors"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"io"
	"math"
	"strings"
	"time"
)

func validateResponse(r io.Reader, operation string) ([]byte, int, error) {
	var raw json.RawMessage
	if err := v1.DecodeResponseStrict(r, &raw); err != nil {
		return nil, 1, err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return nil, 1, errors.New("object required")
	}
	if _, ok := root["error"]; ok {
		var failure v1.ErrorResponse
		if err := v1.DecodeStrict(bytes.NewReader(raw), &failure); err != nil {
			return nil, 1, err
		}
		if len(root) != 2 || failure.Message == "" || v1.Fail(failure.Error).Response.Error != failure.Error {
			return nil, 1, errors.New("invalid failure")
		}
		// Do not echo an untrusted service message. Rebuild from the stable code.
		clean, _ := json.Marshal(v1.Fail(failure.Error).Response)
		return clean, 1, nil
	}
	if operation == "credential.query" {
		return queryResponse(root)
	}
	if err := operationsResponse(operation, raw); err != nil {
		return nil, 1, err
	}
	if err := nonSecretResponse(operation, root); err != nil {
		return nil, 1, err
	}
	return raw, 0, nil
}

func operationsResponse(operation string, raw []byte) error {
	invalid := errors.New("invalid operations result")
	switch operation {
	case "operations.backup":
		var out v1.BackupResult
		if v1.DecodeStrict(bytes.NewReader(raw), &out) != nil || out.Mode != "fixture" || out.Revision < 0 || !strings.HasPrefix(out.BackupID, "backup-") || len(out.BackupID) != 33 {
			return invalid
		}
		for _, c := range strings.TrimPrefix(out.BackupID, "backup-") {
			if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
				return invalid
			}
		}
		if at, err := time.Parse(time.RFC3339Nano, out.CreatedAt); err != nil || at.IsZero() || !strings.HasSuffix(out.CreatedAt, "Z") {
			return invalid
		}
	case "operations.retention":
		var out v1.RetentionResult
		if v1.DecodeStrict(bytes.NewReader(raw), &out) != nil || out.Mode != "fixture" || out.Removed < 0 {
			return invalid
		}
		if at, err := time.Parse(time.RFC3339Nano, out.Cutoff); err != nil || at.IsZero() || !strings.HasSuffix(out.Cutoff, "Z") {
			return invalid
		}
	case "recovery.status", "recovery.open":
		var out v1.RecoveryStatusResult
		if v1.DecodeStrict(bytes.NewReader(raw), &out) != nil || out.Mode != "fixture" || out.Revision < 0 || operation == "recovery.open" && out.RecoveryClosed {
			return invalid
		}
	}
	return nil
}
func queryResponse(root map[string]json.RawMessage) ([]byte, int, error) {
	invalid := func() ([]byte, int, error) { return nil, 1, errors.New("invalid query result") }
	if len(root) != 3 || root["identity"] == nil || root["credentials"] == nil || root["status"] == nil {
		return invalid()
	}
	var identity v1.Identity
	var identityRaw map[string]json.RawMessage
	if json.Unmarshal(root["identity"], &identity) != nil || json.Unmarshal(root["identity"], &identityRaw) != nil {
		return invalid()
	}
	for _, k := range []string{"app_id", "platform", "provider_app_id", "external_shop_id", "provider_shop_id", "brand_id", "internal_shop_id"} {
		var id string
		if json.Unmarshal(identityRaw[k], &id) != nil || id == "" {
			return invalid()
		}
	}
	if identity.Platform != "fixture" {
		return invalid()
	}
	var creds struct {
		App  []v1.CredentialValue `json:"app"`
		Shop []v1.CredentialValue `json:"shop"`
	}
	if err := v1.DecodeStrict(bytes.NewReader(root["credentials"]), &creds); err != nil {
		return invalid()
	}
	if len(creds.App) != 1 || len(creds.Shop) != 1 || creds.App[0].Type != "client_secret" || creds.Shop[0].Type != "shop_token" || creds.App[0].Value == "" || creds.Shop[0].Value == "" {
		return invalid()
	}
	var status map[string]json.RawMessage
	if json.Unmarshal(root["status"], &status) != nil {
		return invalid()
	}
	var result v1.QueryResult
	result.Identity = identity
	result.Credentials.App = creds.App
	result.Credentials.Shop = creds.Shop
	for _, scope := range []string{"app", "shop"} {
		var entries []json.RawMessage
		if json.Unmarshal(status[scope], &entries) != nil || len(entries) != 1 {
			return invalid()
		}
		var s v1.CredentialStatus
		var fields map[string]json.RawMessage
		if json.Unmarshal(entries[0], &s) != nil || json.Unmarshal(entries[0], &fields) != nil {
			return invalid()
		}
		for _, key := range []string{"type", "generation", "available", "expiry_known", "expires_at"} {
			if _, ok := fields[key]; !ok {
				return invalid()
			}
		}
		if err := validateStatusFields(fields); err != nil {
			return invalid()
		}
		typ := "client_secret"
		if scope == "shop" {
			typ = "shop_token"
		}
		if s.Type != typ || s.Generation <= 0 || !s.Available || s.Disabled {
			return invalid()
		}
		if s.ExpiryKnown {
			if s.ExpiresAt == nil {
				return invalid()
			}
			t, err := time.Parse(time.RFC3339, *s.ExpiresAt)
			if err != nil || !t.After(time.Now().UTC()) {
				return invalid()
			}
		} else if s.ExpiresAt != nil || !s.ConfirmedByOperator {
			return invalid()
		}
		if scope == "app" {
			result.Status.App = []v1.CredentialStatus{s}
		} else {
			result.Status.Shop = []v1.CredentialStatus{s}
		}
	}
	clean, _ := json.Marshal(result)
	return clean, 0, nil
}
func nonSecretResponse(operation string, root map[string]json.RawMessage) error {
	fields := map[string][]string{
		"app.list": {"apps", "revision", "total", "offset", "limit"}, "shop.list": {"shops", "revision", "total", "offset", "limit"}, "brand.list": {"brands", "revision", "total", "offset", "limit"}, "internal_shop.list": {"internal_shops", "revision", "total", "offset", "limit"}, "installation.list": {"installations", "revision", "total", "offset", "limit"}, "credential.status": {"statuses", "revision", "total", "offset", "limit"}, "audit.list": {"events", "total", "offset", "limit"}, "health": {"revision", "audit_degraded", "recovery_closed", "operations_degraded", "mode", "deferred"},
		"operations.backup":    {"backup_id", "revision", "audit_gap", "created_at", "mode"},
		"operations.retention": {"removed", "cutoff", "mode"},
		"recovery.status":      {"revision", "recovery_closed", "audit_gap", "mode"},
		"recovery.open":        {"revision", "recovery_closed", "audit_gap", "mode"},
	}
	allowed, ok := fields[operation]
	if !ok {
		allowed = []string{"operation_id", "revision", "generation", "row_revision"}
	}
	allow := map[string]bool{}
	for _, key := range allowed {
		allow[key] = true
	}
	requiredEntries := map[string][]string{"apps": {"app_id", "platform", "provider_app_id", "row_revision"}, "shops": {"external_shop_id", "platform", "provider_shop_id", "row_revision"}, "brands": {"brand_id", "internal_shop_id", "row_revision"}, "internal_shops": {"internal_shop_id", "row_revision"}, "installations": {"app_id", "external_shop_id", "row_revision"}, "statuses": {"scope", "app_id", "type", "generation", "row_revision", "available", "expiry_known", "expires_at"}, "events": {"time", "uid", "actor_name", "operation", "target", "result", "operation_id"}}
	for key, required := range requiredEntries {
		if b, exists := root[key]; exists {
			var entries []map[string]json.RawMessage
			if json.Unmarshal(b, &entries) != nil || entries == nil {
				return errors.New("invalid result list")
			}
			for _, entry := range entries {
				allowedFields := metadataEntryFields(key)
				for field := range entry {
					if !allowedFields[field] {
						return errors.New("unknown metadata field")
					}
				}
				for _, field := range required {
					if entry[field] == nil {
						return errors.New("incomplete metadata")
					}
				}
			}
		}
	}
	for key := range root {
		if !allow[key] {
			return errors.New("unknown response field")
		}
	}
	if !ok {
		var m v1.MutationResult
		b, _ := json.Marshal(root)
		if err := v1.DecodeStrict(bytes.NewReader(b), &m); err != nil || m.OperationID == "" || m.Revision <= 0 || m.Generation < 0 || m.RowRevision <= 0 {
			return errors.New("invalid mutation result")
		}
		if strings.HasPrefix(operation, "credential.") && m.Generation <= 0 {
			return errors.New("missing generation")
		}
		return nil
	}
	for _, key := range allowed {
		if root[key] == nil {
			return errors.New("incomplete result")
		}
	}
	b, _ := json.Marshal(root)
	var tree any
	if json.Unmarshal(b, &tree) != nil {
		return errors.New("invalid result")
	}
	return inspectMetadata(tree)
}
func validateStatusFields(fields map[string]json.RawMessage) error {
	for key, value := range fields {
		switch key {
		case "type":
			var s string
			if json.Unmarshal(value, &s) != nil || s == "" {
				return errors.New("invalid type")
			}
		case "generation", "row_revision":
			var n int64
			if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &n) != nil || n <= 0 {
				return errors.New("invalid version")
			}
		case "available", "expiry_known", "disabled", "confirmed_by_operator":
			var b bool
			if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &b) != nil {
				return errors.New("invalid boolean")
			}
		case "expires_at":
			if !bytes.Equal(value, []byte("null")) {
				var s string
				if json.Unmarshal(value, &s) != nil {
					return errors.New("invalid expiry")
				}
				if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
					return errors.New("invalid expiry")
				}
			}
		}
	}
	return nil
}
func metadataEntryFields(kind string) map[string]bool {
	fields := map[string][]string{
		"apps":           {"app_id", "platform", "provider_app_id", "name", "row_revision", "authorization_epoch"},
		"shops":          {"external_shop_id", "platform", "provider_shop_id", "name", "brand_id", "row_revision"},
		"brands":         {"brand_id", "name", "internal_shop_id", "row_revision"},
		"internal_shops": {"internal_shop_id", "name", "row_revision"},
		"installations":  {"app_id", "external_shop_id", "row_revision"},
		"statuses":       {"scope", "app_id", "external_shop_id", "type", "generation", "row_revision", "available", "disabled", "confirmed_by_operator", "expiry_known", "expires_at"},
		"events":         {"time", "uid", "actor_name", "operation", "target", "result", "error", "operation_id"},
	}
	result := map[string]bool{}
	for _, key := range fields[kind] {
		result[key] = true
	}
	return result
}
func inspectMetadata(node any) error {
	allowed := map[string]bool{}
	for _, key := range []string{"apps", "shops", "brands", "internal_shops", "installations", "statuses", "events", "total", "offset", "limit", "revision", "audit_degraded", "recovery_closed", "operations_degraded", "audit_gap", "backup_id", "created_at", "cutoff", "removed", "mode", "deferred", "id", "platform", "provider_app_id", "name", "authorization_epoch", "row_revision", "provider_shop_id", "brand_id", "internal_shop_id", "app_id", "external_shop_id", "scope", "type", "generation", "available", "disabled", "confirmed_by_operator", "expiry_known", "expires_at", "time", "uid", "actor_name", "operation", "target", "result", "error", "operation_id", "error_code"} {
		allowed[key] = true
	}
	switch n := node.(type) {
	case map[string]any:
		for key, value := range n {
			if !allowed[key] {
				return errors.New("unknown metadata field")
			}
			switch key {
			case "revision", "generation", "row_revision", "authorization_epoch", "total", "offset", "limit", "uid", "removed":
				number, ok := value.(float64)
				if !ok || number < 0 || math.Trunc(number) != number || number > 9007199254740991 {
					return errors.New("invalid metadata number")
				}
				if (key == "generation" || key == "row_revision") && number == 0 {
					return errors.New("invalid version")
				}
			case "available", "disabled", "confirmed_by_operator", "expiry_known", "audit_degraded", "recovery_closed", "operations_degraded", "audit_gap":
				if _, ok := value.(bool); !ok {
					return errors.New("invalid metadata boolean")
				}
			case "expires_at":
				if value != nil {
					if s, ok := value.(string); !ok {
						return errors.New("invalid time")
					} else if _, err := time.Parse(time.RFC3339, s); err != nil {
						return errors.New("invalid time")
					}
				}
			case "deferred":
				entries, ok := value.([]any)
				if !ok {
					return errors.New("invalid deferred array")
				}
				for _, entry := range entries {
					if _, ok := entry.(string); !ok {
						return errors.New("invalid deferred item")
					}
				}
			case "apps", "shops", "brands", "internal_shops", "installations", "statuses", "events":
				if _, ok := value.([]any); !ok {
					return errors.New("invalid metadata array")
				}
			default:
				if _, ok := value.(string); !ok {
					return errors.New("invalid metadata string")
				}
			}
			if err := inspectMetadata(value); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range n {
			if err := inspectMetadata(value); err != nil {
				return err
			}
		}
	}
	return nil
}
