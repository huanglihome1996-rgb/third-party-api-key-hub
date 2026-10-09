package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"time"
)

const Version = "contract/v1"
const MaxRequestBytes = 1 << 20
const MaxResponseBytes = 16 << 20

type ErrorCode string

const (
	ErrInputInvalid       ErrorCode = "input_invalid"
	ErrNotFound           ErrorCode = "not_found"
	ErrSelectionRequired  ErrorCode = "selection_required"
	ErrMappingIncomplete  ErrorCode = "mapping_incomplete"
	ErrMappingMismatch    ErrorCode = "mapping_mismatch"
	ErrPermissionDenied   ErrorCode = "permission_denied"
	ErrCredentialMissing  ErrorCode = "credential_missing"
	ErrCredentialExpired  ErrorCode = "credential_expired"
	ErrCredentialDisabled ErrorCode = "credential_disabled"
	ErrInternal           ErrorCode = "internal_error"
)

type ErrorResponse struct {
	Error   ErrorCode `json:"error"`
	Message string    `json:"message"`
}

func (e *ErrorResponse) ErrorText() string { return e.Message }

// Failure is the typed error returned by the application. Its text is always safe.
type Failure struct{ Response ErrorResponse }

func (f *Failure) Error() string { return f.Response.Message }
func Fail(code ErrorCode) *Failure {
	messages := map[ErrorCode]string{ErrInputInvalid: "invalid request or version conflict", ErrNotFound: "target not found", ErrSelectionRequired: "explicit selection required", ErrMappingIncomplete: "complete ownership mapping required", ErrMappingMismatch: "selection outside requested ownership", ErrPermissionDenied: "permission denied", ErrCredentialMissing: "required credential unavailable", ErrCredentialExpired: "required credential expired", ErrCredentialDisabled: "required credential disabled", ErrInternal: "internal dependency unavailable"}
	m := messages[code]
	if m == "" {
		code = ErrInternal
		m = messages[code]
	}
	return &Failure{Response: ErrorResponse{Error: code, Message: m}}
}
func SafeError(err error) ErrorResponse {
	var f *Failure
	if errors.As(err, &f) {
		return f.Response
	}
	return Fail(ErrInternal).Response
}

// Response is used for non-query successes only. Failures encode ErrorResponse directly.
type Response struct {
	Version     string `json:"version"`
	OperationID string `json:"operation_id"`
	Data        any    `json:"data,omitempty"`
}
type OperationRequest struct {
	Version   string          `json:"version"`
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload"`
}

var knownOperations = map[string]bool{"app.register": true, "app.list": true, "shop.register": true, "shop.list": true, "brand.register": true, "brand.list": true, "internal_shop.register": true, "internal_shop.list": true, "shop.map_brand": true, "shop.unmap_brand": true, "brand.map_internal_shop": true, "brand.unmap_internal_shop": true, "installation.create": true, "installation.list": true, "credential.import": true, "credential.confirm": true, "credential.mark_invalid": true, "credential.disable": true, "credential.restore": true, "credential.status": true, "credential.query": true, "audit.list": true, "health": true}

func ValidateOperationRequest(r OperationRequest) error {
	if r.Version != Version || !knownOperations[r.Operation] || len(r.Payload) == 0 || bytes.Equal(bytes.TrimSpace(r.Payload), []byte("null")) {
		return errors.New("invalid request")
	}
	return nil
}

// DecodeStrict rejects duplicate keys, case aliases, unknown fields and trailing JSON.
// Input errors never contain input values or attacker-controlled field names.
func DecodeStrict(r io.Reader, dst any) error {
	return decodeStrict(r, dst, MaxRequestBytes)
}
func DecodeResponseStrict(r io.Reader, dst any) error { return decodeStrict(r, dst, MaxResponseBytes) }
func decodeStrict(r io.Reader, dst any, limit int) error {
	b, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil || len(b) > limit {
		return errors.New("invalid request")
	}
	if !validUnicodeJSON(b) {
		return errors.New("invalid Unicode")
	}
	if err = walkJSON(b); err != nil {
		return errors.New("invalid JSON")
	}
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer {
		return errors.New("invalid destination")
	}
	if err = exactFields(b, t.Elem()); err != nil {
		return errors.New("invalid fields")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(dst); err != nil {
		return errors.New("invalid JSON")
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func walkJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		if x, ok := t.(json.Delim); ok {
			if x == '{' {
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					s, ok := k.(string)
					if !ok || seen[s] {
						return errors.New("duplicate key")
					}
					seen[s] = true
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			}
			if x == '[' {
				for d.More() {
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			}
			return errors.New("invalid JSON")
		}
		return nil
	}
	if e := walk(); e != nil {
		return e
	}
	_, e := d.Token()
	if e != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

// Only immutable Go type metadata is shared. JSON bytes, decoded values and
// validation decisions always belong to the current call.
var strictFieldTypes sync.Map

func fieldTypes(t reflect.Type) map[string]reflect.Type {
	if cached, ok := strictFieldTypes.Load(t); ok {
		return cached.(map[string]reflect.Type)
	}
	fields := make(map[string]reflect.Type, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		for j, c := range tag {
			if c == ',' {
				tag = tag[:j]
				break
			}
		}
		if tag == "" {
			tag = f.Name
		}
		fields[tag] = f.Type
	}
	actual, _ := strictFieldTypes.LoadOrStore(t, fields)
	return actual.(map[string]reflect.Type)
}

func exactFields(b []byte, t reflect.Type) error {
	if t == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return errors.New("null field")
	}
	if t == reflect.TypeOf(time.Time{}) {
		var at time.Time
		return json.Unmarshal(b, &at)
	}
	switch t.Kind() {
	case reflect.Struct:
		var m map[string]json.RawMessage
		if json.Unmarshal(b, &m) != nil || m == nil {
			return errors.New("object required")
		}
		fields := fieldTypes(t)
		for k, v := range m {
			ft, ok := fields[k]
			if !ok {
				return errors.New("unknown field")
			}
			if ft.Kind() == reflect.Pointer && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				continue
			}
			if e := exactFields(v, ft); e != nil {
				return e
			}
		}
	case reflect.Slice:
		var a []json.RawMessage
		if json.Unmarshal(b, &a) != nil {
			return errors.New("array required")
		}
		for _, v := range a {
			if e := exactFields(v, t.Elem()); e != nil {
				return e
			}
		}
	}
	return nil
}
func RequireFields(payload json.RawMessage, required ...string) error {
	var m map[string]json.RawMessage
	if json.Unmarshal(payload, &m) != nil || m == nil {
		return errors.New("object required")
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return errors.New("required field missing")
		}
	}
	return nil
}

type RegisterAppInput struct {
	AppID         string `json:"app_id"`
	Platform      string `json:"platform"`
	ProviderAppID string `json:"provider_app_id"`
	Name          string `json:"name,omitempty"`
}
type RegisterShopInput struct {
	ExternalShopID string `json:"external_shop_id"`
	Platform       string `json:"platform"`
	ProviderShopID string `json:"provider_shop_id"`
	Name           string `json:"name,omitempty"`
}
type RegisterBrandInput struct {
	BrandID string `json:"brand_id"`
	Name    string `json:"name,omitempty"`
}
type RegisterInternalShopInput struct {
	InternalShopID string `json:"internal_shop_id"`
	Name           string `json:"name,omitempty"`
}
type MapInput struct {
	ExternalShopID   string `json:"external_shop_id,omitempty"`
	BrandID          string `json:"brand_id,omitempty"`
	InternalShopID   string `json:"internal_shop_id,omitempty"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type InstallationInput struct {
	AppID            string `json:"app_id"`
	ExternalShopID   string `json:"external_shop_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type ImportCredentialInput struct {
	Scope              string  `json:"scope"`
	AppID              string  `json:"app_id"`
	ExternalShopID     string  `json:"external_shop_id,omitempty"`
	Type               string  `json:"type"`
	Value              string  `json:"value"`
	Replace            bool    `json:"replace"`
	ExpectedGeneration int64   `json:"expected_generation,omitempty"`
	ExpiryKnown        bool    `json:"expiry_known"`
	ExpiresAt          *string `json:"expires_at"`
	Confirm            bool    `json:"confirm"`
}
type CredentialRefInput struct {
	Scope              string `json:"scope"`
	AppID              string `json:"app_id"`
	ExternalShopID     string `json:"external_shop_id,omitempty"`
	Type               string `json:"type"`
	ExpectedGeneration int64  `json:"expected_generation,omitempty"`
	Reason             string `json:"reason,omitempty"`
}
type QueryInput struct {
	By             string `json:"by"`
	AppID          string `json:"app_id,omitempty"`
	ExternalShopID string `json:"external_shop_id,omitempty"`
	BrandID        string `json:"brand_id,omitempty"`
	InternalShopID string `json:"internal_shop_id,omitempty"`
}
type ListInput struct {
	AppID          string `json:"app_id,omitempty"`
	ExternalShopID string `json:"external_shop_id,omitempty"`
	Offset         int    `json:"offset,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}
type AuditListInput struct {
	From        *string `json:"from,omitempty"`
	To          *string `json:"to,omitempty"`
	Operation   string  `json:"operation,omitempty"`
	Target      string  `json:"target,omitempty"`
	Result      string  `json:"result,omitempty"`
	OperationID string  `json:"operation_id,omitempty"`
	Offset      int     `json:"offset,omitempty"`
	Limit       int     `json:"limit,omitempty"`
}
type CredentialValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}
type CredentialStatus struct {
	Type                string  `json:"type"`
	Generation          int64   `json:"generation"`
	RowRevision         int64   `json:"row_revision,omitempty"`
	Available           bool    `json:"available"`
	Disabled            bool    `json:"disabled,omitempty"`
	ConfirmedByOperator bool    `json:"confirmed_by_operator,omitempty"`
	ExpiryKnown         bool    `json:"expiry_known"`
	ExpiresAt           *string `json:"expires_at"`
}
type Identity struct {
	AppID          string `json:"app_id"`
	Platform       string `json:"platform"`
	ProviderAppID  string `json:"provider_app_id"`
	ExternalShopID string `json:"external_shop_id"`
	ProviderShopID string `json:"provider_shop_id"`
	BrandID        string `json:"brand_id"`
	InternalShopID string `json:"internal_shop_id"`
}
type QueryResult struct {
	Identity    Identity `json:"identity"`
	Credentials struct {
		App  []CredentialValue `json:"app"`
		Shop []CredentialValue `json:"shop"`
	} `json:"credentials"`
	Status struct {
		App  []CredentialStatus `json:"app"`
		Shop []CredentialStatus `json:"shop"`
	} `json:"status"`
}
type MutationResult struct {
	OperationID string `json:"operation_id"`
	Revision    int64  `json:"revision"`
	Generation  int64  `json:"generation,omitempty"`
	RowRevision int64  `json:"row_revision,omitempty"`
}

type StatusEntry struct {
	Scope               string  `json:"scope"`
	AppID               string  `json:"app_id"`
	ExternalShopID      string  `json:"external_shop_id,omitempty"`
	Type                string  `json:"type"`
	Generation          int64   `json:"generation"`
	RowRevision         int64   `json:"row_revision"`
	Available           bool    `json:"available"`
	Disabled            bool    `json:"disabled,omitempty"`
	ConfirmedByOperator bool    `json:"confirmed_by_operator,omitempty"`
	ExpiryKnown         bool    `json:"expiry_known"`
	ExpiresAt           *string `json:"expires_at"`
}
type StatusResult struct {
	Statuses []StatusEntry `json:"statuses"`
	Revision int64         `json:"revision"`
	Total    int           `json:"total"`
	Offset   int           `json:"offset"`
	Limit    int           `json:"limit"`
}
type HealthResult struct {
	OperationsDegraded bool     `json:"operations_degraded"`
	RecoveryClosed     bool     `json:"recovery_closed"`
	Revision           int64    `json:"revision"`
	AuditDegraded      bool     `json:"audit_degraded"`
	Mode               string   `json:"mode"`
	Deferred           []string `json:"deferred"`
}

type PageMetadata struct {
	Total    int   `json:"total"`
	Offset   int   `json:"offset"`
	Limit    int   `json:"limit"`
	Revision int64 `json:"revision"`
}
