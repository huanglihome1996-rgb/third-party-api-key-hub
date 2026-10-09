//go:build linux

package backup

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/adapters/sqlite"
	"github.com/third-party-api-key-hub/internal/application"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"github.com/third-party-api-key-hub/internal/domain"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Manager, application.State, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	key := filepath.Join(root, "key")
	p, err := protector.NewFixture(key)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.New(filepath.Join(root, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	audit, err := sqlite.NewAudit(filepath.Join(root, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { audit.Close() })
	state := application.EmptyState()
	state.Revision = 1
	state.Apps["a"] = domain.App{ID: "a", Platform: "fixture", ProviderAppID: "pa", RowRevision: 1}
	state.Shops["s"] = domain.ExternalShop{ID: "s", Platform: "fixture", ProviderShopID: "ps", BrandID: "b", RowRevision: 1}
	state.Brands["b"] = domain.Brand{ID: "b", InternalShopID: "i", RowRevision: 1}
	state.Internals["i"] = domain.InternalShop{ID: "i", RowRevision: 1}
	state.Installations["a|s"] = domain.Installation{AppID: "a", ExternalShopID: "s", RowRevision: 1}
	for _, c := range []domain.Credential{{Scope: "app", AppID: "a", Type: "client_secret"}, {Scope: "shop", AppID: "a", ExternalShopID: "s", Type: "shop_token", Disabled: true, DisabledGeneration: 1}} {
		c.Generation = 1
		c.RowRevision = 1
		c.ConfirmedGeneration = 1
		c.Ciphertext, err = p.Seal("current-credential", c.Scope+"|a|"+c.ExternalShopID, c.Type, "fixture-value", 1)
		if err != nil {
			t.Fatal(err)
		}
		state.Credentials[c.Scope+"|a|"+c.ExternalShopID+"|"+c.Type] = c
	}
	if err = repo.Save(ctx, state); err != nil {
		t.Fatal(err)
	}
	for _, event := range []application.AuditEvent{
		{Time: time.Date(2025, 1, 2, 3, 4, 5, 123456789, time.UTC), Operation: "credential.disable", OperationID: "op-historical-disable", Result: "success", Target: "shop|a|s|shop_token"},
		{Time: time.Date(2025, 1, 3, 4, 5, 6, 987654321, time.UTC), Operation: "local.reject", OperationID: "op-historical-reject", Result: "failure", Target: "unknown", Error: "permission_denied"},
	} {
		if err = audit.Record(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	return &Manager{Root: filepath.Join(root, "backups"), KeyFile: key, Repo: repo, Audit: audit}, state, root
}

func assertAuditHistory(t *testing.T, expected, actual []application.AuditEvent) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatal("audit history length changed")
	}
	for i, event := range expected {
		got := actual[i]
		if !event.Time.Equal(got.Time) || event.Operation != got.Operation || event.OperationID != got.OperationID || event.Result != got.Result || event.Target != got.Target || event.Error != got.Error {
			t.Fatal("audit history changed")
		}
	}
}

func TestReplacingIndependentKeyCannotPublishUnrestorableBackup(t *testing.T) {
	m, s, root := fixture(t)
	ctx := context.Background()
	good, err := m.Backup(ctx, s.Revision, false, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(m.Root, good.BackupID)
	before, _ := os.ReadFile(filepath.Join(source, "manifest.json"))
	original, _ := os.ReadFile(m.KeyFile)
	wrong := filepath.Join(root, "replacement-key")
	if _, err = protector.NewFixture(wrong); err != nil {
		t.Fatal(err)
	}
	replacement, _ := os.ReadFile(wrong)
	if err = os.WriteFile(m.KeyFile, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Backup(ctx, s.Revision, false, func() bool { return true }); err == nil {
		t.Fatal("unrestorable snapshot published")
	}
	entries, _ := os.ReadDir(m.Root)
	if len(entries) != 1 {
		t.Fatal("failed backup published or retained staging")
	}
	after, _ := os.ReadFile(filepath.Join(source, "manifest.json"))
	if string(before) != string(after) {
		t.Fatal("previous package modified")
	}
	if err = os.WriteFile(m.KeyFile, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Validate(ctx, source, m.KeyFile); err != nil {
		t.Fatal("previous recovery point lost", err)
	}
}

type failingAudit struct{}

func (failingAudit) Record(context.Context, application.AuditEvent) error {
	return errors.New("fake-dependency")
}

func TestRealAuditFailurePersistsGapAndAppearsInSnapshot(t *testing.T) {
	m, _, root := fixture(t)
	ctx := context.Background()
	p, _ := protector.NewExistingFixture(m.KeyFile)
	e, err := application.NewEngine(m.Repo, p, failingAudit{}, func(a application.Actor) application.Actor { return a })
	if err != nil {
		t.Fatal(err)
	}
	e.ConfigureOperations(m)
	actor := application.Actor{Manage: true}
	if _, err = e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "app.list", Payload: json.RawMessage(`{}`)}, actor); err != nil {
		t.Fatal(err)
	}
	actual, err := m.Repo.Load(ctx)
	if err != nil || !actual.AuditGap {
		t.Fatal("audit failure gap not durable")
	}
	out, err := e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "operations.backup", Payload: json.RawMessage(`{}`)}, actor)
	if err != nil {
		t.Fatal(err)
	}
	result := out.(v1.BackupResult)
	if !result.AuditGap {
		t.Fatal("gap lost in backup result")
	}
	target := filepath.Join(root, "gap-restored")
	if err = Restore(ctx, filepath.Join(m.Root, result.BackupID), target, m.KeyFile); err != nil {
		t.Fatal(err)
	}
	repo, _ := sqlite.New(filepath.Join(target, "business.sqlite"))
	defer repo.Close()
	restarted, err := application.NewEngine(repo, p, m.Audit, func(a application.Actor) application.Actor { return a })
	if err != nil {
		t.Fatal(err)
	}
	health, err := restarted.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "health", Payload: json.RawMessage(`{}`)}, actor)
	if err != nil || !health.(v1.HealthResult).AuditDegraded {
		t.Fatal("restart hid audit gap")
	}
}

func TestBackupRestoreIntegrityKeyBindingsAndClosedRestart(t *testing.T) {
	m, state, root := fixture(t)
	ctx := context.Background()
	out, err := m.Backup(ctx, state.Revision, true, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(m.Root, out.BackupID)
	history, err := m.Audit.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshotAudit, err := sqlite.NewAudit(filepath.Join(source, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	snapshotHistory, err := snapshotAudit.List(ctx)
	snapshotAudit.Close()
	if err != nil {
		t.Fatal(err)
	}
	assertAuditHistory(t, history, snapshotHistory)
	manifest, _, err := Validate(ctx, source, m.KeyFile)
	if err != nil || !manifest.AuditGap {
		t.Fatal("invalid manifest", err)
	}
	target := filepath.Join(root, "restored")
	if err = Restore(ctx, source, target, m.KeyFile); err != nil {
		t.Fatal(err)
	}
	restoredAudit, err := sqlite.NewAudit(filepath.Join(target, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	restoredHistory, err := restoredAudit.List(ctx)
	restoredAudit.Close()
	if err != nil {
		t.Fatal(err)
	}
	assertAuditHistory(t, history, restoredHistory)
	repo, err := sqlite.New(filepath.Join(target, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := repo.Load(ctx)
	repo.Close()
	if err != nil || !restored.RecoveryClosed || !restored.AuditGap || restored.Revision != 2 || !restored.Credentials["shop|a|s|shop_token"].Disabled {
		t.Fatal("restored lifecycle lost", err)
	}
	if _, err = os.Lstat(filepath.Join(target, "fixture.key")); !os.IsNotExist(err) {
		t.Fatal("key copied")
	}
	if err = Restore(ctx, source, target, m.KeyFile); err == nil {
		t.Fatal("existing target overwritten")
	}
	if err = Restore(ctx, source, filepath.Join(root, "missing-key"), filepath.Join(root, "absent")); err == nil {
		t.Fatal("missing key accepted")
	}
	wrong := filepath.Join(root, "wrong-key")
	protector.NewFixture(wrong)
	if err = Restore(ctx, source, filepath.Join(root, "wrong"), wrong); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = os.Lstat(filepath.Join(root, "wrong")); !os.IsNotExist(err) {
		t.Fatal("failed target published")
	}
	if err = os.Symlink(source, filepath.Join(root, "source-link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Validate(ctx, filepath.Join(root, "source-link"), m.KeyFile); err == nil {
		t.Fatal("symlink accepted")
	}
	raw, _ := os.ReadFile(filepath.Join(source, "manifest.json"))
	var tampered Manifest
	json.Unmarshal(raw, &tampered)
	tampered.Build = "other"
	bad, _ := json.Marshal(tampered)
	os.WriteFile(filepath.Join(source, "manifest.json"), bad, 0600)
	if _, _, err = Validate(ctx, source, m.KeyFile); err == nil {
		t.Fatal("build binding ignored")
	}
	os.WriteFile(filepath.Join(source, "manifest.json"), raw, 0600)
	f, _ := os.OpenFile(filepath.Join(source, "business.sqlite"), os.O_APPEND|os.O_WRONLY, 0600)
	f.Write([]byte("corrupt"))
	f.Close()
	if _, _, err = Validate(ctx, source, m.KeyFile); err == nil {
		t.Fatal("file hash ignored")
	}
}

func TestFailedBackupKeepsPublishedPackageAndRevocation(t *testing.T) {
	m, s, _ := fixture(t)
	ctx := context.Background()
	good, err := m.Backup(ctx, s.Revision, false, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(m.Root, good.BackupID)
	before, _ := os.ReadFile(filepath.Join(source, "manifest.json"))
	if _, err = m.Backup(ctx, s.Revision, false, func() bool { return false }); err != domain.ErrPermission {
		t.Fatal("revoked backup published", err)
	}
	m.Audit.Close()
	if _, err = m.Backup(ctx, s.Revision, false, func() bool { return true }); err == nil {
		t.Fatal("failed audit snapshot published")
	}
	after, _ := os.ReadFile(filepath.Join(source, "manifest.json"))
	if string(before) != string(after) {
		t.Fatal("existing backup modified")
	}
	entries, _ := os.ReadDir(m.Root)
	if len(entries) != 1 {
		t.Fatal("incomplete package retained")
	}
}

func TestBackupRejectsExtraFilesAndCipherBindingEvenWithUpdatedHashes(t *testing.T) {
	m, s, _ := fixture(t)
	ctx := context.Background()
	out, err := m.Backup(ctx, s.Revision, false, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(m.Root, out.BackupID)
	os.WriteFile(filepath.Join(source, "fixture.key"), []byte("fake"), 0600)
	if _, _, err = Validate(ctx, source, m.KeyFile); err == nil {
		t.Fatal("extra file accepted")
	}
	os.Remove(filepath.Join(source, "fixture.key"))
	repo, err := sqlite.New(filepath.Join(source, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ = repo.Load(ctx)
	c := s.Credentials["shop|a|s|shop_token"]
	c.Ciphertext = s.Credentials["app|a||client_secret"].Ciphertext
	s.Credentials["shop|a|s|shop_token"] = c
	s.Revision++
	repo.Save(ctx, s)
	repo.Close()
	raw, _ := os.ReadFile(filepath.Join(source, "manifest.json"))
	var manifest Manifest
	json.Unmarshal(raw, &manifest)
	manifest.Revision = s.Revision
	manifest.Files["business.sqlite"], _ = digest(filepath.Join(source, "business.sqlite"))
	raw, _ = json.Marshal(manifest)
	os.WriteFile(filepath.Join(source, "manifest.json"), raw, 0600)
	if _, _, err = Validate(ctx, source, m.KeyFile); err == nil {
		t.Fatal("credential binding accepted")
	}
}

func TestRestoredDisabledCredentialRemainsDisabledAfterOpen(t *testing.T) {
	m, s, root := fixture(t)
	ctx := context.Background()
	out, err := m.Backup(ctx, s.Revision, false, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "closed")
	if err = Restore(ctx, filepath.Join(m.Root, out.BackupID), target, m.KeyFile); err != nil {
		t.Fatal(err)
	}
	repo, _ := sqlite.New(filepath.Join(target, "business.sqlite"))
	defer repo.Close()
	audit, _ := sqlite.NewAudit(filepath.Join(target, "audit.sqlite"))
	defer audit.Close()
	p, _ := protector.NewExistingFixture(m.KeyFile)
	engine, err := application.NewEngine(repo, p, audit, func(a application.Actor) application.Actor { return a })
	if err != nil {
		t.Fatal(err)
	}
	refs := []v1.CredentialRefInput{}
	for _, c := range s.Credentials {
		refs = append(refs, v1.CredentialRefInput{Scope: c.Scope, AppID: c.AppID, ExternalShopID: c.ExternalShopID, Type: c.Type, ExpectedGeneration: c.Generation})
	}
	raw, _ := json.Marshal(v1.RecoveryOpenInput{ExpectedRevision: 2, AccountsVerified: true, OwnershipVerified: true, ValuesVerified: true, Credentials: refs})
	if _, err = engine.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "recovery.open", Payload: raw}, application.Actor{Manage: true}); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(v1.QueryInput{By: "external_shop", ExternalShopID: "s"})
	outQuery, err := engine.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "credential.query", Payload: raw}, application.Actor{Read: true})
	if outQuery != nil || v1.SafeError(err).Error != v1.ErrCredentialDisabled {
		t.Fatal("disabled released")
	}
}

func TestIntermediateSnapshotRestoresClosedAndRequiresRepairBeforeOpen(t *testing.T) {
	for _, broken := range []string{"mapping", "required"} {
		m, s, root := fixture(t)
		ctx := context.Background()
		// Retain ordinary availability; the test isolates incomplete-state repair.
		c := s.Credentials["shop|a|s|shop_token"]
		c.Disabled = false
		s.Credentials["shop|a|s|shop_token"] = c
		if broken == "mapping" {
			sh := s.Shops["s"]
			sh.BrandID = ""
			s.Shops["s"] = sh
		} else {
			delete(s.Credentials, "shop|a|s|shop_token")
		}
		s.Revision++
		if err := m.Repo.Save(ctx, s); err != nil {
			t.Fatal(err)
		}
		out, err := m.Backup(ctx, s.Revision, false, func() bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "intermediate")
		if err = Restore(ctx, filepath.Join(m.Root, out.BackupID), target, m.KeyFile); err != nil {
			t.Fatal("legal intermediate state could not restore", err)
		}
		repo, _ := sqlite.New(filepath.Join(target, "business.sqlite"))
		defer repo.Close()
		audit, _ := sqlite.NewAudit(filepath.Join(target, "audit.sqlite"))
		defer audit.Close()
		p, _ := protector.NewExistingFixture(m.KeyFile)
		e, err := application.NewEngine(repo, p, audit, func(a application.Actor) application.Actor { return a })
		if err != nil {
			t.Fatal(err)
		}
		actor := application.Actor{Read: true, Manage: true}
		call := func(op string, in any) (any, error) {
			raw, _ := json.Marshal(in)
			return e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: op, Payload: raw}, actor)
		}
		review := func() v1.RecoveryOpenInput {
			st, _ := repo.Load(ctx)
			refs := []v1.CredentialRefInput{}
			for _, c := range st.Credentials {
				refs = append(refs, v1.CredentialRefInput{Scope: c.Scope, AppID: c.AppID, ExternalShopID: c.ExternalShopID, Type: c.Type, ExpectedGeneration: c.Generation})
			}
			return v1.RecoveryOpenInput{ExpectedRevision: st.Revision, AccountsVerified: true, OwnershipVerified: true, ValuesVerified: true, Credentials: refs}
		}
		if out, err := call("recovery.open", review()); err == nil || out != nil {
			t.Fatal("incomplete state opened")
		}
		if broken == "mapping" {
			_, err = call("shop.map_brand", v1.MapInput{ExternalShopID: "s", BrandID: "b", ExpectedRevision: 1})
		} else {
			_, err = call("credential.import", v1.ImportCredentialInput{Scope: "shop", AppID: "a", ExternalShopID: "s", Type: "shop_token", Value: "fixture-repaired", Confirm: true})
		}
		if err != nil {
			t.Fatal("repair rejected", err)
		}
		if out, err := call("credential.query", v1.QueryInput{By: "external_shop", ExternalShopID: "s"}); out != nil || v1.SafeError(err).Error != v1.ErrCredentialDisabled {
			t.Fatal("repair implicitly opened reads")
		}
		if _, err = call("recovery.open", review()); err != nil {
			t.Fatal("repaired state failed review", err)
		}
		if _, err = call("credential.query", v1.QueryInput{By: "external_shop", ExternalShopID: "s"}); err != nil {
			t.Fatal(err)
		}
	}
}
