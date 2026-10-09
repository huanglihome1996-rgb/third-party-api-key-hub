package application

import (
	"context"
	"encoding/json"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"reflect"
	"strings"
	"testing"
	"time"
)

type expiryProtector struct {
	*fakeProtector
	expiry time.Time
}

func (p *expiryProtector) Open(a, b, c, d string, g int64) (string, error) {
	v, err := p.fakeProtector.Open(a, b, c, d, g)
	time.Sleep(time.Until(p.expiry.Add(2 * time.Millisecond)))
	return v, err
}
func TestReviewFractionalExpiryRoundtrip(t *testing.T) {
	e, _, _, _ := newFixture(t)
	seedValues(t, e)
	in := appImport()
	in.Replace = true
	in.ExpectedGeneration = 1
	in.ExpiryKnown = true
	s := "2030-01-02T03:04:05.123456789Z"
	in.ExpiresAt = &s
	run(t, e, "credential.import", in)
	r := run(t, e, "credential.query", query()).(v1.QueryResult)
	if r.Status.App[0].ExpiresAt == nil || *r.Status.App[0].ExpiresAt != s {
		t.Fatalf("expiry precision lost: %+v", r.Status.App)
	}
}
func TestReviewExpiryDuringDecryptNoOutput(t *testing.T) {
	e, _, p, _ := newFixture(t)
	seedValues(t, e)
	exp := time.Now().Add(100 * time.Millisecond).UTC()
	in := appImport()
	in.Replace = true
	in.ExpectedGeneration = 1
	in.ExpiryKnown = true
	s := exp.Format(time.RFC3339Nano)
	in.ExpiresAt = &s
	run(t, e, "credential.import", in)
	e.protector = &expiryProtector{fakeProtector: p, expiry: exp}
	code(t, e, "credential.query", query(), v1.ErrCredentialExpired)
}
func TestReviewForbiddenPresentFields(t *testing.T) {
	e, _, _, _ := newFixture(t)
	seedValues(t, e)
	for _, tt := range []struct{ op, body string }{{"app.list", `{"app_id":""}`}, {"shop.unmap_brand", `{"external_shop_id":"shop-1","brand_id":"","expected_revision":2}`}, {"credential.disable", `{"scope":"app","app_id":"app-1","type":"client_secret","expected_generation":0}`}, {"credential.query", `{"by":"external_shop","external_shop_id":"shop-1","app_id":""}`}, {"credential.import", `{"scope":"app","app_id":"app-1","type":"refresh_token","value":"fixture-value","replace":false,"expected_generation":0,"expiry_known":false,"expires_at":null,"confirm":true}`}} {
		res, err := e.Execute(context.Background(), v1.OperationRequest{Version: v1.Version, Operation: tt.op, Payload: json.RawMessage(tt.body)}, manager)
		if res != nil || v1.SafeError(err).Error != v1.ErrInputInvalid {
			t.Errorf("%s accepted forbidden fields: %v", tt.op, err)
		}
	}
}
func TestReviewFailedBusinessAuditTarget(t *testing.T) {
	e, _, _, a := newFixture(t)
	seedValues(t, e)
	code(t, e, "app.register", v1.RegisterAppInput{AppID: "app-1", Platform: "fixture", ProviderAppID: "duplicate"}, v1.ErrInputInvalid)
	event := a.events[len(a.events)-1]
	if event.Target != "app_id:app-1" || event.Result != "failure" {
		t.Fatal(event)
	}
}

func TestReviewProviderPortUsesTrustedCombination(t *testing.T) {
	e, repo, p, a := newFixture(t)
	profiles := StaticProviderRegistry{"fictional-second": ProviderProfile{Credentials: []CredentialDefinition{{Scope: "app", Type: "api_secret", Purpose: "business", Required: true}, {Scope: "app", Type: "admin_secret", Purpose: "management"}, {Scope: "shop", Type: "shop_access", Purpose: "business", Required: true}}}}
	var err error
	e, err = NewEngine(repo, p, a, func(actor Actor) Actor { return actor }, profiles)
	if err != nil {
		t.Fatal(err)
	}
	run(t, e, "app.register", v1.RegisterAppInput{AppID: "app-1", Platform: "fictional-second", ProviderAppID: "provider-1"})
	run(t, e, "shop.register", v1.RegisterShopInput{ExternalShopID: "shop-1", Platform: "fictional-second", ProviderShopID: "provider-shop-1"})
	run(t, e, "brand.register", v1.RegisterBrandInput{BrandID: "brand-1"})
	run(t, e, "internal_shop.register", v1.RegisterInternalShopInput{InternalShopID: "internal-1"})
	run(t, e, "shop.map_brand", v1.MapInput{ExternalShopID: "shop-1", BrandID: "brand-1", ExpectedRevision: 1})
	run(t, e, "brand.map_internal_shop", v1.MapInput{BrandID: "brand-1", InternalShopID: "internal-1", ExpectedRevision: 1})
	in := appImport()
	in.Type = "api_secret"
	run(t, e, "credential.import", in)
	in.Type = "admin_secret"
	in.Value = "fixture-second-management"
	run(t, e, "credential.import", in)
	shop := shopImport()
	shop.Type = "shop_access"
	run(t, e, "credential.import", shop)
	r := run(t, e, "credential.query", query()).(v1.QueryResult)
	if r.Credentials.App[0].Type != "api_secret" || r.Credentials.Shop[0].Type != "shop_access" || len(r.Credentials.App) != 1 {
		t.Fatal(r)
	}
	code(t, e, "credential.import", appImport(), v1.ErrInputInvalid)
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "fixture-second-management") {
		t.Fatal("profile management type leaked")
	}
	run(t, e, "credential.disable", v1.CredentialRefInput{Scope: "app", AppID: "app-1", Type: "api_secret"})
	code(t, e, "credential.query", query(), v1.ErrCredentialDisabled)
}

func TestReviewListAndStatusPagination(t *testing.T) {
	e, _, _, _ := newFixture(t)
	seedValues(t, e)
	run(t, e, "app.register", v1.RegisterAppInput{AppID: "app-2", Platform: "fixture", ProviderAppID: "provider-2"})
	run(t, e, "shop.register", v1.RegisterShopInput{ExternalShopID: "shop-2", Platform: "fixture", ProviderShopID: "provider-shop-2"})
	run(t, e, "brand.register", v1.RegisterBrandInput{BrandID: "brand-2"})
	run(t, e, "internal_shop.register", v1.RegisterInternalShopInput{InternalShopID: "internal-2"})
	run(t, e, "installation.create", v1.InstallationInput{AppID: "app-2", ExternalShopID: "shop-2", ExpectedRevision: 1})
	cases := []struct{ op, key string }{{"app.list", "apps"}, {"shop.list", "shops"}, {"brand.list", "brands"}, {"internal_shop.list", "internal_shops"}, {"installation.list", "installations"}}
	for _, tt := range cases {
		whole := run(t, e, tt.op, v1.ListInput{}).(map[string]any)
		b, _ := json.Marshal(whole)
		var m map[string]json.RawMessage
		json.Unmarshal(b, &m)
		var want []json.RawMessage
		json.Unmarshal(m[tt.key], &want)
		got := []json.RawMessage{}
		for offset := 0; offset < 2; offset++ {
			r := run(t, e, tt.op, v1.ListInput{Offset: offset, Limit: 1}).(map[string]any)
			if r["total"] != 2 || r["offset"] != offset || r["limit"] != 1 || r["revision"] != e.state.Revision {
				t.Fatal(r)
			}
			b, _ = json.Marshal(r)
			json.Unmarshal(b, &m)
			var values []json.RawMessage
			json.Unmarshal(m[tt.key], &values)
			if len(values) != 1 {
				t.Fatal(r)
			}
			got = append(got, values...)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("%s pages differ", tt.op)
		}
	}
	whole := run(t, e, "credential.status", v1.ListInput{}).(v1.StatusResult)
	var got []v1.StatusEntry
	for offset := 0; offset < 2; offset++ {
		r := run(t, e, "credential.status", v1.ListInput{Offset: offset, Limit: 1}).(v1.StatusResult)
		if r.Total != 2 || r.Limit != 1 || r.Offset != offset || r.Revision != e.state.Revision {
			t.Fatal(r)
		}
		got = append(got, r.Statuses...)
	}
	if !reflect.DeepEqual(whole.Statuses, got) {
		t.Fatal("status pages differ")
	}
	empty := run(t, e, "credential.status", v1.ListInput{Offset: int(^uint(0) >> 1), Limit: 1000}).(v1.StatusResult)
	if len(empty.Statuses) != 0 {
		t.Fatal(empty)
	}
}
func TestReviewAuditFiltersPaginationAndSafeRejections(t *testing.T) {
	e, _, _, a := newFixture(t)
	base := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	a.events = []AuditEvent{{Time: base, Operation: "app.register", Target: "app_id:app-1", Result: "success", OperationID: "one"}, {Time: base.Add(time.Second), Operation: "app.register", Target: "app_id:app-1", Result: "failure", Error: "input_invalid", OperationID: "two"}, {Time: base.Add(2 * time.Second), Operation: "app.register", Target: "app_id:app-1", Result: "failure", Error: "input_invalid", OperationID: "three"}, {Time: base.Add(3 * time.Second), Operation: "app.register", Target: "app_id:app-2", Result: "failure", OperationID: "four"}}
	from := base.Add(time.Second).Format(time.RFC3339Nano)
	to := base.Add(2 * time.Second).Format(time.RFC3339Nano)
	in := v1.AuditListInput{From: &from, To: &to, Operation: "app.register", Target: "app_id:app-1", Result: "failure", Limit: 1}
	r := run(t, e, "audit.list", in).(AuditListResult)
	if r.Total != 2 || len(r.Events) != 1 || r.Events[0].OperationID != "two" || r.Offset != 0 || r.Limit != 1 {
		t.Fatal(r)
	}
	in.Offset = 1
	r = run(t, e, "audit.list", in).(AuditListResult)
	if r.Total != 2 || len(r.Events) != 1 || r.Events[0].OperationID != "three" || r.Offset != 1 {
		t.Fatal(r)
	}
	in.Offset = 0
	in.OperationID = "three"
	r = run(t, e, "audit.list", in).(AuditListResult)
	if r.Total != 1 || r.Events[0].OperationID != "three" {
		t.Fatal(r)
	}
	res, err := e.Execute(context.Background(), v1.OperationRequest{Version: v1.Version, Operation: "credential.mark_invalid", Payload: []byte(`{"app_id":"a|b","scope":"app","type":"client_secret","expected_generation":1,"reason":"fixture-secret"}`)}, manager)
	if res != nil || err == nil {
		t.Fatal(res, err)
	}
	event := a.events[len(a.events)-1]
	if event.Target != "unknown" {
		t.Fatal(event)
	}
	b, _ := json.Marshal(event)
	if strings.Contains(string(b), "fixture-secret") || strings.Contains(string(b), "a|b") {
		t.Fatal("rejection echoed unsafe data")
	}
	e.RecordBoundaryRejection(context.Background(), manager, v1.ErrInputInvalid)
	event = a.events[len(a.events)-1]
	if event.Operation != "local.reject" || event.Target != "unknown" || event.Error != "input_invalid" {
		t.Fatal(event)
	}
	a.fail = true
	e.RecordBoundaryRejection(context.Background(), manager, v1.ErrInternal)
	if !e.AuditDegraded() {
		t.Fatal("boundary audit failure not degraded")
	}
}
