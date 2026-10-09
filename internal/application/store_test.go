package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"github.com/third-party-api-key-hub/internal/domain"
	"strings"
	"sync"
	"testing"
	"time"
)

type memRepo struct {
	mu    sync.Mutex
	state State
	fail  bool
}

func (r *memRepo) Load(context.Context) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return clone(r.state), nil
}
func (r *memRepo) Save(_ context.Context, s State) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("fake-dependency-sensitive")
	}
	r.state = clone(s)
	return nil
}

type fakeProtector struct {
	mu                 sync.Mutex
	values             map[string]string
	failSeal, failOpen bool
	opens              int
}

func (p *fakeProtector) Seal(purpose, target, typ, value string, g int64) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failSeal {
		return "", errors.New(value)
	}
	k := fmt.Sprintf("opaque-%s-%s-%s-%d", purpose, target, typ, g)
	p.values[k] = value
	return k, nil
}
func (p *fakeProtector) Open(purpose, target, typ, cipher string, g int64) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opens++
	if p.failOpen {
		return "", errors.New("fake-do-not-leak")
	}
	k := fmt.Sprintf("opaque-%s-%s-%s-%d", purpose, target, typ, g)
	if k != cipher {
		return "", errors.New("binding failure")
	}
	v, ok := p.values[k]
	if !ok {
		return "", errors.New("missing ciphertext")
	}
	return v, nil
}

type memoryAudit struct {
	mu     sync.Mutex
	events []AuditEvent
	fail   bool
}

func (a *memoryAudit) Record(_ context.Context, v AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail {
		return errors.New("audit unavailable")
	}
	a.events = append(a.events, v)
	return nil
}
func (a *memoryAudit) List(context.Context) ([]AuditEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AuditEvent{}, a.events...), nil
}

var manager = Actor{UID: 10, Name: "fixture-operator", Read: true, Manage: true}

func newFixture(t *testing.T) (*Engine, *memRepo, *fakeProtector, *memoryAudit) {
	t.Helper()
	repo := &memRepo{state: EmptyState()}
	p := &fakeProtector{values: map[string]string{}}
	a := &memoryAudit{}
	e, err := NewEngine(repo, p, a, func(actor Actor) Actor { return actor })
	if err != nil {
		t.Fatal(err)
	}
	return e, repo, p, a
}
func request(op string, in any) v1.OperationRequest {
	b, _ := json.Marshal(in)
	return v1.OperationRequest{Version: v1.Version, Operation: op, Payload: b}
}
func run(t *testing.T, e *Engine, op string, in any) any {
	t.Helper()
	res, err := e.Execute(context.Background(), request(op, in), manager)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return res
}
func code(t *testing.T, e *Engine, op string, in any, want v1.ErrorCode) {
	t.Helper()
	res, err := e.Execute(context.Background(), request(op, in), manager)
	if err == nil || v1.SafeError(err).Error != want {
		t.Fatalf("%s got %v %+v want %s", op, err, res, want)
	}
	if res != nil {
		t.Fatal("partial output")
	}
}
func seed(t *testing.T, e *Engine) {
	t.Helper()
	run(t, e, "app.register", v1.RegisterAppInput{AppID: "app-1", Platform: "fixture", ProviderAppID: "provider-1"})
	run(t, e, "shop.register", v1.RegisterShopInput{ExternalShopID: "shop-1", Platform: "fixture", ProviderShopID: "provider-shop-1"})
	run(t, e, "brand.register", v1.RegisterBrandInput{BrandID: "brand-1"})
	run(t, e, "internal_shop.register", v1.RegisterInternalShopInput{InternalShopID: "internal-1"})
	run(t, e, "shop.map_brand", v1.MapInput{ExternalShopID: "shop-1", BrandID: "brand-1", ExpectedRevision: 1})
	run(t, e, "brand.map_internal_shop", v1.MapInput{BrandID: "brand-1", InternalShopID: "internal-1", ExpectedRevision: 1})
	run(t, e, "installation.create", v1.InstallationInput{AppID: "app-1", ExternalShopID: "shop-1", ExpectedRevision: 1})
}
func appImport() v1.ImportCredentialInput {
	return v1.ImportCredentialInput{Scope: "app", AppID: "app-1", Type: "client_secret", Value: "fixture-app-value", Confirm: true}
}
func shopImport() v1.ImportCredentialInput {
	return v1.ImportCredentialInput{Scope: "shop", AppID: "app-1", ExternalShopID: "shop-1", Type: "shop_token", Value: "fixture-shop-value", Confirm: true}
}
func appRef(g int64) v1.CredentialRefInput {
	return v1.CredentialRefInput{Scope: "app", AppID: "app-1", Type: "client_secret", ExpectedGeneration: g}
}
func query() v1.QueryInput { return v1.QueryInput{By: "external_shop", ExternalShopID: "shop-1"} }
func seedValues(t *testing.T, e *Engine) {
	seed(t, e)
	run(t, e, "credential.import", appImport())
	run(t, e, "credential.import", shopImport())
}
func TestManualLifecycleAndRestart(t *testing.T) {
	e, repo, p, a := newFixture(t)
	seedValues(t, e)
	r := run(t, e, "credential.query", query()).(v1.QueryResult)
	if r.Credentials.App[0].Value != "fixture-app-value" || r.Credentials.Shop[0].Value != "fixture-shop-value" || r.Status.App[0].Generation != 1 {
		t.Fatal(r)
	}
	run(t, e, "credential.import", v1.ImportCredentialInput{Scope: "app", AppID: "app-1", Type: "refresh_token", Value: "fixture-management", Confirm: true})
	r = run(t, e, "credential.query", query()).(v1.QueryResult)
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "fixture-management") {
		t.Fatal("management secret exposed")
	}
	e2, err := NewEngine(repo, p, a, func(x Actor) Actor { return x })
	if err != nil {
		t.Fatal(err)
	}
	r = run(t, e2, "credential.query", query()).(v1.QueryResult)
	if r.Identity.InternalShopID != "internal-1" {
		t.Fatal(r)
	}
	b, _ = json.Marshal(repo.state)
	if strings.Contains(string(b), "fixture-app-value") {
		t.Fatal("plaintext persisted")
	}
}
func TestConflictRollbackAndPersistenceFailure(t *testing.T) {
	e, repo, p, _ := newFixture(t)
	seedValues(t, e)
	rev := repo.state.Revision
	code(t, e, "app.register", v1.RegisterAppInput{AppID: "app-2", Platform: "fixture", ProviderAppID: "provider-1"}, v1.ErrInputInvalid)
	if repo.state.Revision != rev {
		t.Fatal("conflict changed state")
	}
	in := appImport()
	in.Replace = true
	in.ExpectedGeneration = 1
	in.Value = "fixture-replacement"
	repo.fail = true
	code(t, e, "credential.import", in, v1.ErrInternal)
	r := run(t, e, "credential.query", query()).(v1.QueryResult)
	if r.Credentials.App[0].Value != "fixture-app-value" || r.Status.App[0].Generation != 1 {
		t.Fatal("failed commit published")
	}
	repo.fail = false
	p.failSeal = true
	code(t, e, "credential.import", in, v1.ErrInternal)
	p.failSeal = false
	r = run(t, e, "credential.query", query()).(v1.QueryResult)
	if r.Credentials.App[0].Value != "fixture-app-value" {
		t.Fatal("seal failure published")
	}
}
func TestPermissionFirstAndRecheck(t *testing.T) {
	e, _, p, _ := newFixture(t)
	bad := v1.OperationRequest{Operation: "not-real", Payload: []byte(`bad`)}
	res, err := e.Execute(context.Background(), bad, Actor{})
	if res != nil || v1.SafeError(err).Error != v1.ErrPermissionDenied {
		t.Fatal(res, err)
	}
	seedValues(t, e)
	n := 0
	e.authorize = func(a Actor) Actor {
		n++
		if n == 2 {
			a.Read = false
		}
		return a
	}
	res, err = e.Execute(context.Background(), request("credential.query", query()), manager)
	if res != nil || v1.SafeError(err).Error != v1.ErrPermissionDenied {
		t.Fatal(res, err)
	}
	if p.opens != 2 {
		t.Fatalf("expected complete decrypt before recheck got %d", p.opens)
	}
	n = 0
	e.authorize = func(a Actor) Actor {
		n++
		if n == 2 {
			a.Manage = false
		}
		return a
	}
	code(t, e, "brand.register", v1.RegisterBrandInput{BrandID: "denied"}, v1.ErrPermissionDenied)
	e.authorize = func(a Actor) Actor { return a }
	if _, ok := e.state.Brands["denied"]; ok {
		t.Fatal("manage revocation committed")
	}
}
func TestExpiryConfirmationReplacementAndStaleInvalid(t *testing.T) {
	e, _, _, _ := newFixture(t)
	seed(t, e)
	in := appImport()
	in.Confirm = false
	run(t, e, "credential.import", in)
	run(t, e, "credential.import", shopImport())
	code(t, e, "credential.query", query(), v1.ErrCredentialMissing)
	run(t, e, "credential.confirm", appRef(1))
	run(t, e, "credential.query", query())
	in.Replace = true
	in.ExpectedGeneration = 1
	in.Value = "fixture-new"
	run(t, e, "credential.import", in)
	code(t, e, "credential.query", query(), v1.ErrCredentialMissing)
	old := appRef(1)
	old.Reason = "rejected"
	code(t, e, "credential.mark_invalid", old, v1.ErrInputInvalid)
	run(t, e, "credential.confirm", appRef(2))
	run(t, e, "credential.query", query())
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	in.ExpectedGeneration = 2
	in.ExpiryKnown = true
	in.ExpiresAt = &past
	in.Confirm = true
	run(t, e, "credential.import", in)
	code(t, e, "credential.query", query(), v1.ErrCredentialExpired)
	code(t, e, "credential.confirm", appRef(2), v1.ErrInputInvalid)
}
func TestDisableReplacementAndRestore(t *testing.T) {
	e, repo, p, a := newFixture(t)
	seedValues(t, e)
	run(t, e, "credential.disable", appRef(0))
	run(t, e, "credential.confirm", appRef(1))
	code(t, e, "credential.query", query(), v1.ErrCredentialDisabled)
	code(t, e, "credential.restore", appRef(1), v1.ErrInputInvalid)
	in := appImport()
	in.Replace = true
	in.ExpectedGeneration = 1
	in.Value = "fixture-new"
	run(t, e, "credential.import", in)
	code(t, e, "credential.query", query(), v1.ErrCredentialDisabled)
	run(t, e, "shop.unmap_brand", v1.MapInput{ExternalShopID: "shop-1", ExpectedRevision: 2})
	code(t, e, "credential.restore", appRef(2), v1.ErrMappingIncomplete)
	run(t, e, "shop.map_brand", v1.MapInput{ExternalShopID: "shop-1", BrandID: "brand-1", ExpectedRevision: 3})
	run(t, e, "credential.restore", appRef(2))
	run(t, e, "credential.query", query())
	e2, err := NewEngine(repo, p, a, func(x Actor) Actor { return x })
	if err != nil {
		t.Fatal(err)
	}
	run(t, e2, "credential.query", query())
	run(t, e2, "credential.disable", appRef(0))
	code(t, e2, "credential.restore", appRef(2), v1.ErrInputInvalid)
}
func TestCurrentInvalidClearsRecovery(t *testing.T) {
	e, _, _, _ := newFixture(t)
	seedValues(t, e)
	run(t, e, "credential.disable", appRef(0))
	in := appImport()
	in.Replace = true
	in.ExpectedGeneration = 1
	run(t, e, "credential.import", in)
	ref := appRef(2)
	ref.Reason = "invalid"
	run(t, e, "credential.mark_invalid", ref)
	code(t, e, "credential.restore", appRef(2), v1.ErrInputInvalid)
	c := e.state.Credentials[ckey("app", "app-1", "", "client_secret")]
	if c.RecoveryBasisGeneration != 0 || !c.Disabled {
		t.Fatal(c)
	}
}
func TestSelectionAndOwnershipMismatch(t *testing.T) {
	e, _, _, _ := newFixture(t)
	seedValues(t, e)
	run(t, e, "shop.register", v1.RegisterShopInput{ExternalShopID: "shop-2", Platform: "fixture", ProviderShopID: "provider-shop-2"})
	run(t, e, "shop.map_brand", v1.MapInput{ExternalShopID: "shop-2", BrandID: "brand-1", ExpectedRevision: 1})
	code(t, e, "credential.query", v1.QueryInput{By: "brand", BrandID: "brand-1"}, v1.ErrSelectionRequired)
	run(t, e, "credential.query", v1.QueryInput{By: "brand", BrandID: "brand-1", ExternalShopID: "shop-1"})
	run(t, e, "brand.register", v1.RegisterBrandInput{BrandID: "brand-2"})
	code(t, e, "credential.query", v1.QueryInput{By: "brand", BrandID: "brand-2", ExternalShopID: "shop-1"}, v1.ErrMappingMismatch)
	code(t, e, "credential.query", v1.QueryInput{By: "internal_shop", InternalShopID: "internal-1", ExternalShopID: "shop-2", AppID: "app-1"}, v1.ErrMappingMismatch)
	run(t, e, "app.register", v1.RegisterAppInput{AppID: "app-2", Platform: "fixture", ProviderAppID: "provider-2"})
	run(t, e, "installation.create", v1.InstallationInput{AppID: "app-2", ExternalShopID: "shop-1", ExpectedRevision: 1})
	code(t, e, "credential.query", query(), v1.ErrSelectionRequired)
	q := query()
	q.AppID = "app-1"
	run(t, e, "credential.query", q)
	code(t, e, "brand.map_internal_shop", v1.MapInput{BrandID: "brand-2", InternalShopID: "internal-1", ExpectedRevision: 1}, v1.ErrInputInvalid)
}
func TestNoPartialSecretsAuditDegradedAndStatus(t *testing.T) {
	e, _, p, a := newFixture(t)
	seedValues(t, e)
	p.failOpen = true
	res, err := e.Execute(context.Background(), request("credential.query", query()), manager)
	if res != nil || v1.SafeError(err).Error != v1.ErrInternal || strings.Contains(err.Error(), "fake-do-not-leak") {
		t.Fatal(res, err)
	}
	p.failOpen = false
	st := run(t, e, "credential.status", v1.ListInput{})
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "fixture-app-value") || strings.Contains(string(b), "Ciphertext") {
		t.Fatal("status secret")
	}
	a.fail = true
	run(t, e, "brand.register", v1.RegisterBrandInput{BrandID: "brand-audit"})
	h := run(t, e, "health", struct{}{}).(v1.HealthResult)
	if !h.AuditDegraded {
		t.Fatal(h)
	}
	a.fail = false
	events := run(t, e, "audit.list", v1.AuditListInput{}).(AuditListResult)
	if events.Total == 0 {
		t.Fatal(events)
	}
	b, _ = json.Marshal(events)
	if strings.Contains(string(b), "fixture-app-value") {
		t.Fatal("audit leaked")
	}
}
func TestStrictPayloadAndProfiles(t *testing.T) {
	e, _, _, _ := newFixture(t)
	cases := []string{`null`, `{}`, `{"app_id":"a","platform":"fixture","provider_app_id":"p","Value":"fake"}`, `{"App_ID":"a","platform":"fixture","provider_app_id":"p"}`, `{"app_id":"a","app_id":"b","platform":"fixture","provider_app_id":"p"}`, `{"app_id":"a|b","platform":"fixture","provider_app_id":"p"}`, `{"app_id":" a","platform":"fixture","provider_app_id":"p"}`, `{"app_id":"a","platform":"tiktok","provider_app_id":"p"}`, `{"app_id":null,"platform":"fixture","provider_app_id":"p"}`, `{"app_id":"a","platform":"fixture","provider_app_id":"p","required_app_types":[]}`}
	for _, b := range cases {
		_, err := e.Execute(context.Background(), v1.OperationRequest{Version: v1.Version, Operation: "app.register", Payload: []byte(b)}, manager)
		if v1.SafeError(err).Error != v1.ErrInputInvalid {
			t.Fatalf("accepted %s: %v", b, err)
		}
	}
	seed(t, e)
	in := appImport()
	in.Type = "unknown"
	code(t, e, "credential.import", in, v1.ErrInputInvalid)
	in = appImport()
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	in.ExpiresAt = &future
	code(t, e, "credential.import", in, v1.ErrInputInvalid)
	in.ExpiryKnown = true
	offset := "2030-01-01T00:00:00+08:00"
	in.ExpiresAt = &offset
	code(t, e, "credential.import", in, v1.ErrInputInvalid)
}
func TestAllListOperationsAndContextCancellation(t *testing.T) {
	e, _, _, _ := newFixture(t)
	seedValues(t, e)
	for _, op := range []string{"app.list", "shop.list", "brand.list", "internal_shop.list", "installation.list"} {
		run(t, e, op, v1.ListInput{})
	}
	run(t, e, "credential.query", v1.QueryInput{By: "internal_shop", InternalShopID: "internal-1"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := e.Execute(ctx, request("credential.query", query()), manager)
	if res != nil || v1.SafeError(err).Error != v1.ErrInternal {
		t.Fatal(res, err)
	}
	if _, err = NewEngine(nil, nil, nil, nil); err == nil {
		t.Fatal("nil ports")
	}
}
func TestDomainAvailableInvalidAndRecovery(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Second)
	c := domain.Credential{Generation: 2, ExpiryKnown: true, ExpiresAt: &past, ConfirmedGeneration: 2}
	if c.Available(now) != domain.ErrExpired {
		t.Fatal(c)
	}
	c.ExpiresAt = nil
	if c.Available(now) != domain.ErrExpired {
		t.Fatal("invalid expiry")
	}
	c.ExpiryKnown = false
	c.Disabled = true
	c.DisabledGeneration = 1
	c.RecoveryBasisGeneration = 2
	c.RecoveryBasisOperationID = "op"
	if !c.CanRestore() {
		t.Fatal("recovery")
	}
}

func TestConcurrentQueriesAndCASImports(t *testing.T) {
	e, _, _, _ := newFixture(t)
	seedValues(t, e)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if res, err := e.Execute(context.Background(), request("credential.query", query()), manager); err != nil || res == nil {
					t.Errorf("query failure %v", err)
				}
			}
		}()
	}
	success := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := appImport()
			in.Replace = true
			in.ExpectedGeneration = 1
			_, err := e.Execute(context.Background(), request("credential.import", in), manager)
			if err != nil && v1.SafeError(err).Error != v1.ErrInputInvalid {
				t.Error(err)
			}
			success <- err == nil
		}()
	}
	wg.Wait()
	close(success)
	count := 0
	for ok := range success {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("CAS commits %d", count)
	}
}
