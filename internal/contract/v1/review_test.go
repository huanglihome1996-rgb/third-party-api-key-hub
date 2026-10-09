package v1

import (
	"bytes"
	"strings"
	"testing"
)

func TestReviewRejectInvalidUnicode(t *testing.T) {
	bad := [][]byte{append([]byte(`{"name":"`), append([]byte{0xff}, []byte(`"}`)...)...), []byte(`{"name":"\uD800"}`), []byte(`{"name":"\uDC00"}`), []byte(`{"name":"\uD800\uD800"}`), []byte(`{"name":"\uDC00\uD800"}`)}
	for _, b := range bad {
		var v RegisterBrandInput
		if DecodeStrict(bytes.NewReader(b), &v) == nil {
			t.Errorf("invalid Unicode accepted: %q", b)
		}
	}
	for _, b := range []string{`{"name":"\uD83D\uDE00"}`, `{"name":"\\uD800"}`, `{"name":"\\\\uDC00"}`, `{"name":"中文"}`} {
		var v RegisterBrandInput
		if err := DecodeStrict(strings.NewReader(b), &v); err != nil {
			t.Errorf("valid Unicode %s: %v", b, err)
		}
	}
}

func TestReviewOperationPresenceAndResponseBudget(t *testing.T) {
	for _, tt := range []struct{ op, body string }{{"app.list", `{"app_id":""}`}, {"brand.unmap_internal_shop", `{"brand_id":"b","internal_shop_id":"","expected_revision":1}`}, {"credential.disable", `{"scope":"app","app_id":"a","type":"client_secret","expected_generation":0}`}, {"credential.confirm", `{"scope":"app","app_id":"a","type":"client_secret","expected_generation":1,"reason":""}`}, {"credential.status", `{"app_id":""}`}, {"installation.list", `{"limit":0}`}, {"app.list", `{"limit":1001}`}, {"credential.import", `{"scope":"app","app_id":"a","external_shop_id":"","replace":false,"expected_generation":0}`}} {
		if ValidateOperationPayload(OperationRequest{Operation: tt.op, Payload: []byte(tt.body)}) == nil {
			t.Errorf("allowed forbidden fields %s %s", tt.op, tt.body)
		}
	}
	for _, tt := range []struct{ op, body string }{{"app.list", `{"offset":0,"limit":1000}`}, {"installation.list", `{"app_id":"a","offset":0,"limit":1}`}, {"credential.disable", `{"scope":"app","app_id":"a","type":"client_secret"}`}} {
		if err := ValidateOperationPayload(OperationRequest{Operation: tt.op, Payload: []byte(tt.body)}); err != nil {
			t.Fatal(tt, err)
		}
	}
	body := `{"name":"` + strings.Repeat("a", MaxRequestBytes) + `"}`
	var out RegisterBrandInput
	if DecodeStrict(strings.NewReader(body), &out) == nil {
		t.Fatal("request limit expanded")
	}
	if err := DecodeResponseStrict(strings.NewReader(body), &out); err != nil {
		t.Fatal("response budget", err)
	}
	if DecodeResponseStrict(strings.NewReader(strings.Repeat(" ", MaxResponseBytes+1)), &out) == nil {
		t.Fatal("response limit bypass")
	}
}
