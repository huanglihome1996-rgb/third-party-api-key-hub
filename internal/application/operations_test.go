package application

import (
	"context"
	"encoding/json"
	"errors"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"github.com/third-party-api-key-hub/internal/domain"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoveryClosedAcrossEngineRestart(t *testing.T) {
	e, r, p, a := newFixture(t)
	seedValues(t, e)
	e.state.RecoveryClosed = true
	r.state = clone(e.state)
	e, err := NewEngine(r, p, a, func(a Actor) Actor { return a })
	if err != nil {
		t.Fatal(err)
	}
	code(t, e, "credential.query", v1.QueryInput{By: "external_shop", AppID: "app-1", ExternalShopID: "shop-1"}, v1.ErrCredentialDisabled)
	refs := []v1.CredentialRefInput{}
	for _, c := range e.state.Credentials {
		refs = append(refs, v1.CredentialRefInput{Scope: c.Scope, AppID: c.AppID, ExternalShopID: c.ExternalShopID, Type: c.Type, ExpectedGeneration: c.Generation})
	}
	in := v1.RecoveryOpenInput{ExpectedRevision: e.state.Revision, AccountsVerified: true, OwnershipVerified: true, ValuesVerified: true, Credentials: refs}
	bad := in
	bad.ExpectedRevision++
	code(t, e, "recovery.open", bad, v1.ErrInputInvalid)
	bad = in
	bad.Credentials = refs[:len(refs)-1]
	code(t, e, "recovery.open", bad, v1.ErrInputInvalid)
	run(t, e, "recovery.open", in)
	if e.state.RecoveryClosed || r.state.RecoveryClosed {
		t.Fatal("open not persisted")
	}
	run(t, e, "credential.query", v1.QueryInput{By: "external_shop", AppID: "app-1", ExternalShopID: "shop-1"})
}

type failedOperations struct{ err error }

func (o failedOperations) Backup(context.Context, int64, bool, func() bool) (v1.BackupResult, error) {
	return v1.BackupResult{}, o.err
}
func (o failedOperations) Retention(context.Context, time.Time, func() bool) (v1.RetentionResult, error) {
	return v1.RetentionResult{}, o.err
}

func TestManualOperationsDependencyFailureDegradesAndCountsSafeAlerts(t *testing.T) {
	for _, op := range []string{"operations.backup", "operations.retention"} {
		e, _, _, _ := newFixture(t)
		e.ConfigureOperations(failedOperations{err: errors.New("fake-private-dependency")})
		_, err := e.Execute(context.Background(), request(op, struct{}{}), Actor{})
		if v1.SafeError(err).Error != v1.ErrPermissionDenied || e.operationsDegraded.Load() || e.DrainOperationsFailures() != 0 {
			t.Fatal("permission denial degraded dependency health")
		}
		code(t, e, op, struct{}{}, v1.ErrInternal)
		if !e.operationsDegraded.Load() || e.DrainOperationsFailures() != 1 || e.DrainOperationsFailures() != 0 {
			t.Fatal("dependency failure alert absent")
		}
		e, _, _, _ = newFixture(t)
		e.ConfigureOperations(failedOperations{err: domain.ErrPermission})
		code(t, e, op, struct{}{}, v1.ErrPermissionDenied)
		if e.operationsDegraded.Load() || e.DrainOperationsFailures() != 0 {
			t.Fatal("revocation degraded dependency health")
		}
	}
}

func TestRestoreRejectsDuplicateIdentitiesOwnersAndInvalidRowRevisions(t *testing.T) {
	for _, invalid := range []string{"app_identity", "shop_identity", "internal_owner", "app_revision", "shop_revision", "brand_revision", "internal_revision", "installation_revision"} {
		e, _, p, _ := newFixture(t)
		seedValues(t, e)
		s := clone(e.state)
		switch invalid {
		case "app_identity":
			a := s.Apps["app-1"]
			a.ID = "app-2"
			s.Apps[a.ID] = a
		case "shop_identity":
			sh := s.Shops["shop-1"]
			sh.ID = "shop-2"
			s.Shops[sh.ID] = sh
		case "internal_owner":
			b := s.Brands["brand-1"]
			b.ID = "brand-2"
			s.Brands[b.ID] = b
		case "app_revision":
			a := s.Apps["app-1"]
			a.RowRevision = 0
			s.Apps[a.ID] = a
		case "shop_revision":
			sh := s.Shops["shop-1"]
			sh.RowRevision = -1
			s.Shops[sh.ID] = sh
		case "brand_revision":
			b := s.Brands["brand-1"]
			b.RowRevision = 0
			s.Brands[b.ID] = b
		case "internal_revision":
			i := s.Internals["internal-1"]
			i.RowRevision = 0
			s.Internals[i.ID] = i
		case "installation_revision":
			i := s.Installations["app-1|shop-1"]
			i.RowRevision = 0
			s.Installations["app-1|shop-1"] = i
		}
		if err := ValidateRestoreState(s, p, FixtureProfiles()); err == nil {
			t.Fatalf("restore accepted %s", invalid)
		}
		if err := ValidateRecoveryState(s, p, FixtureProfiles()); err == nil {
			t.Fatalf("open accepted %s", invalid)
		}
	}
}

type heldAudit struct {
	entered, release chan struct{}
	calls            atomic.Int32
}

func (a *heldAudit) Record(context.Context, AuditEvent) error {
	if a.calls.Add(1) == 1 {
		close(a.entered)
		<-a.release
	}
	return nil
}

type captureOperations struct{ started chan struct{} }

func (o *captureOperations) Backup(context.Context, int64, bool, func() bool) (v1.BackupResult, error) {
	close(o.started)
	return v1.BackupResult{}, nil
}
func (o *captureOperations) Retention(context.Context, time.Time, func() bool) (v1.RetentionResult, error) {
	return v1.RetentionResult{}, nil
}

func TestBackupDrainsDeferredBusinessAndBoundaryAudit(t *testing.T) {
	for _, boundary := range []bool{false, true} {
		e, _, _, _ := newFixture(t)
		a := &heldAudit{entered: make(chan struct{}), release: make(chan struct{})}
		e.audit = a
		o := &captureOperations{started: make(chan struct{})}
		e.ConfigureOperations(o)
		done := make(chan struct{})
		go func() {
			defer close(done)
			if boundary {
				e.RecordBoundaryRejection(context.Background(), manager, v1.ErrInputInvalid)
			} else {
				_, _ = e.Execute(context.Background(), request("health", struct{}{}), manager)
			}
		}()
		<-a.entered
		backupDone := make(chan struct{})
		go func() {
			defer close(backupDone)
			_, _ = e.Execute(context.Background(), request("operations.backup", struct{}{}), manager)
		}()
		select {
		case <-o.started:
			t.Fatal("snapshot raced admitted audit")
		case <-time.After(20 * time.Millisecond):
		}
		close(a.release)
		<-done
		select {
		case <-o.started:
		case <-time.After(time.Second):
			t.Fatal("snapshot did not resume")
		}
		<-backupDone
	}
}

func TestRecoveryOpenRechecksPermissionAndKeepsClosed(t *testing.T) {
	e, r, p, a := newFixture(t)
	seedValues(t, e)
	e.state.RecoveryClosed = true
	r.state = clone(e.state)
	var calls atomic.Int32
	e, err := NewEngine(r, p, a, func(actor Actor) Actor { actor.Manage = calls.Add(1) == 1; return actor })
	if err != nil {
		t.Fatal(err)
	}
	refs := []v1.CredentialRefInput{appRef(1), {Scope: "shop", AppID: "app-1", ExternalShopID: "shop-1", Type: "shop_token", ExpectedGeneration: 1}}
	in := v1.RecoveryOpenInput{ExpectedRevision: e.state.Revision, AccountsVerified: true, OwnershipVerified: true, ValuesVerified: true, Credentials: refs}
	code(t, e, "recovery.open", in, v1.ErrPermissionDenied)
	if !e.state.RecoveryClosed || !r.state.RecoveryClosed {
		t.Fatal("revocation opened recovery")
	}
}

func TestRecoveryReviewRequiresCompleteChainsAndBindings(t *testing.T) {
	for _, broken := range []string{"mapping", "required", "binding"} {
		e, r, _, _ := newFixture(t)
		seedValues(t, e)
		e.state.RecoveryClosed = true
		switch broken {
		case "mapping":
			sh := e.state.Shops["shop-1"]
			sh.BrandID = ""
			e.state.Shops["shop-1"] = sh
		case "required":
			delete(e.state.Credentials, "shop|app-1|shop-1|shop_token")
		case "binding":
			c := e.state.Credentials["app|app-1||client_secret"]
			c.Ciphertext = "fake-wrong"
			e.state.Credentials["app|app-1||client_secret"] = c
		}
		r.state = clone(e.state)
		refs := []v1.CredentialRefInput{}
		for _, c := range e.state.Credentials {
			refs = append(refs, v1.CredentialRefInput{Scope: c.Scope, AppID: c.AppID, ExternalShopID: c.ExternalShopID, Type: c.Type, ExpectedGeneration: c.Generation})
		}
		in := v1.RecoveryOpenInput{ExpectedRevision: e.state.Revision, AccountsVerified: true, OwnershipVerified: true, ValuesVerified: true, Credentials: refs}
		out, err := e.Execute(context.Background(), request("recovery.open", in), manager)
		if err == nil || out != nil || !e.state.RecoveryClosed || !r.state.RecoveryClosed {
			t.Fatalf("%s opened recovery", broken)
		}
	}
}

func TestOperationsPermissionBeforePayload(t *testing.T) {
	e, _, _, _ := newFixture(t)
	for _, op := range []string{"operations.backup", "operations.retention", "recovery.status", "recovery.open"} {
		_, err := e.Execute(context.Background(), v1.OperationRequest{Version: v1.Version, Operation: op, Payload: json.RawMessage(`{"path":"fake-private"}`)}, Actor{})
		if v1.SafeError(err).Error != v1.ErrPermissionDenied {
			t.Fatalf("%s permission order", op)
		}
	}
}
