package main

import (
	"bytes"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"strings"
	"testing"
)

func TestInvalidInputNeverEchoesFieldOrValue(t *testing.T) {
	for _, input := range []string{`{"secret-token-should-not-echo":"fixture-sensitive"}`, `{"version":"contract/v1","operation":"bad","payload":{"value":"fixture-sensitive"}}`, `null`, `{}`, `{"version":"contract/v1","version":"duplicate"}`} {
		var out bytes.Buffer
		if code := run(strings.NewReader(input), &out, "missing"); code != 1 {
			t.Fatal("accepted invalid")
		}
		if strings.Contains(out.String(), "fixture-sensitive") || strings.Contains(out.String(), "secret-token") || !strings.Contains(out.String(), `"error":"input_invalid"`) {
			t.Fatal("unsafe output", out.String())
		}
	}
}
func TestResponseRejectsSecretOnError(t *testing.T) {
	for _, r := range []string{`{"error":"internal_error","message":"bad","value":"fixture-secret"}`, `{"error":"bad_code","message":"bad"}`, `{"error":"internal_error"}`, `{"error":"internal_error","message":"bad","error":"not_found"}`} {
		if _, _, err := validateResponse(strings.NewReader(r), "credential.query"); err == nil {
			t.Fatal("accepted invalid failure")
		}
	}
	r, code, err := validateResponse(strings.NewReader(`{"error":"internal_error","message":"fixture-secret"}`), "credential.query")
	if err != nil || code != 1 || bytes.Contains(r, []byte("fixture-secret")) {
		t.Fatal("did not rebuild safe error")
	}
}
func TestQueryMetadataCompatibility(t *testing.T) {
	input := `{"identity":{"app_id":"a","platform":"fixture","provider_app_id":"pa","external_shop_id":"s","provider_shop_id":"ps","brand_id":"b","internal_shop_id":"i","new_metadata":"ignore"},"credentials":{"app":[{"type":"client_secret","value":"fixture-app"}],"shop":[{"type":"shop_token","value":"fixture-shop"}]},"status":{"new_metadata":true,"app":[{"type":"client_secret","generation":1,"available":true,"expiry_known":false,"expires_at":null,"confirmed_by_operator":true,"new_metadata":"ignore"}],"shop":[{"type":"shop_token","generation":1,"available":true,"expiry_known":false,"expires_at":null,"confirmed_by_operator":true}]}}`
	result, code, err := validateResponse(strings.NewReader(input), "credential.query")
	if err != nil || code != 0 || bytes.Contains(result, []byte("new_metadata")) {
		t.Fatal("metadata not stripped", err)
	}
	for _, bad := range []string{strings.Replace(input, `"value":"fixture-app"`, `"value":"fixture-app","unknown":"secret"`, 1), strings.Replace(input, `"type":"client_secret"`, `"type":"refresh_token"`, 1), strings.Replace(input, `"generation":1`, `"generation":0`, 1), strings.Replace(input, `"generation":1`, `"generation":1,"row_revision":-1`, 1), strings.Replace(input, `"available":true`, `"available":null`, 1), strings.Replace(input, `"available":true`, `"available":false`, 1), input[:len(input)-1]} {
		if _, _, err := validateResponse(strings.NewReader(bad), "credential.query"); err == nil {
			t.Fatal("invalid query accepted")
		}
	}
}

func TestMalformedMetadataAndMutationResponses(t *testing.T) {
	for _, tt := range []struct{ operation, response string }{
		{"credential.import", `{"operation_id":"o","revision":1,"generation":-1,"row_revision":1}`},
		{"credential.import", `{"operation_id":"o","revision":1,"row_revision":1}`},
		{"app.register", `{"operation_id":"o","revision":1}`},
		{"health", `{"revision":0,"audit_degraded":false,"mode":"fixture","deferred":[true]}`},
		{"app.list", `{"apps":[{"app_id":"a","platform":"fixture","provider_app_id":"pa","row_revision":1,"target":"fixture-hidden"}],"revision":1,"total":1,"offset":0,"limit":100}`},
	} {
		if _, _, err := validateResponse(strings.NewReader(tt.response), tt.operation); err == nil {
			t.Fatalf("accepted malformed %s", tt.operation)
		}
	}
}

func TestQuerySelectionIdentity(t *testing.T) {
	i := v1.Identity{AppID: "a", ExternalShopID: "s", BrandID: "b", InternalShopID: "i"}
	for _, q := range []v1.QueryInput{{AppID: "other"}, {ExternalShopID: "other"}, {BrandID: "other"}, {InternalShopID: "other"}} {
		if matchesSelection(q, i) {
			t.Fatal("accepted another selected identity")
		}
	}
	if !matchesSelection(v1.QueryInput{AppID: "a", ExternalShopID: "s", BrandID: "b", InternalShopID: "i"}, i) {
		t.Fatal("rejected selected identity")
	}
}

func TestInvalidOperationFieldsRejectedBeforeConnection(t *testing.T) {
	var out bytes.Buffer
	code := run(strings.NewReader(`{"version":"contract/v1","operation":"app.list","payload":{"app_id":""}}`), &out, "missing")
	if code != 1 || !strings.Contains(out.String(), `"error":"input_invalid"`) {
		t.Fatal(code, out.String())
	}
}

func TestListMetadataResponse(t *testing.T) {
	r := `{"apps":[{"app_id":"a","platform":"fixture","provider_app_id":"pa","name":"","row_revision":3,"authorization_epoch":0}],"revision":9,"total":1,"offset":0,"limit":100}`
	if _, _, err := validateResponse(strings.NewReader(r), "app.list"); err != nil {
		t.Fatal(err)
	}
}
