//go:build linux

package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/third-party-api-key-hub/internal/adapters/backup"
	"github.com/third-party-api-key-hub/internal/adapters/local"
	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/adapters/sqlite"
	"github.com/third-party-api-key-hub/internal/application"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"github.com/third-party-api-key-hub/internal/domain"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitializationLockAndExplicitMissingKeyNeverTouchStorage(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine, os.Args = oldFlags, oldArgs }()
	configure := func(args ...string) {
		flag.CommandLine = flag.NewFlagSet("hubd-test", flag.ContinueOnError)
		flag.CommandLine.SetOutput(io.Discard)
		os.Args = append([]string{"hubd-test"}, args...)
	}
	dir := filepath.Join(t.TempDir(), "new-instance")
	release, err := local.AcquireInstanceLock(dir + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	configure("--fixture", "--state-dir", dir)
	if err = run(); err == nil {
		t.Fatal("second initializer admitted")
	}
	release()
	if _, err = os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("second instance touched target")
	}
	configure("--fixture", "--state-dir", dir, "--fixture-key-file", filepath.Join(dir, "fixture.key"))
	if err = run(); err == nil {
		t.Fatal("missing explicit key generated")
	}
	for _, name := range []string{"fixture.key", "business.sqlite", "audit.sqlite"} {
		if _, err = os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatal("storage touched before key validation")
		}
	}
}

func TestAdmissionRejectsWrongExistingKeyAndPersistsLostAuditGap(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	key := filepath.Join(dir, "fixture.key")
	p, err := protector.NewFixture(key)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.New(filepath.Join(dir, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	s := application.EmptyState()
	s.Revision = 1
	s.Apps["a"] = domain.App{ID: "a", Platform: "fixture", ProviderAppID: "pa", RowRevision: 1}
	cipher, err := p.Seal("current-credential", "app|a|", "client_secret", "fixture-existing", 1)
	if err != nil {
		t.Fatal(err)
	}
	s.Credentials["app|a||client_secret"] = domain.Credential{Scope: "app", AppID: "a", Type: "client_secret", Generation: 1, RowRevision: 1, Ciphertext: cipher}
	if err = repo.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	wrongPath := filepath.Join(dir, "wrong.key")
	wrong, err := protector.NewFixture(wrongPath)
	if err != nil {
		t.Fatal(err)
	}
	oldFlags, oldArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine, os.Args = oldFlags, oldArgs }()
	flag.CommandLine = flag.NewFlagSet("hubd-test", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{"hubd-test", "--fixture", "--state-dir", dir, "--fixture-key-file", wrongPath}
	if err = run(); err == nil {
		t.Fatal("wrong valid key admitted persisted ciphertext")
	}
	if _, err = os.Lstat(filepath.Join(dir, "hub.sock")); !os.IsNotExist(err) {
		t.Fatal("listener created before validation")
	}
	repo, err = sqlite.New(filepath.Join(dir, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err = validateAdmission(repo, wrong, filepath.Join(dir, "audit.sqlite"), true); err == nil {
		t.Fatal("wrong key admitted")
	}
	loaded, _ := repo.Load(ctx)
	if loaded.AuditGap {
		t.Fatal("failed binding check modified history")
	}
	// Simulate loss of an existing independent audit database, not a new instance.
	auditPath := filepath.Join(dir, "audit.sqlite")
	audit, err := sqlite.NewAudit(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	audit.Close()
	if err = os.Remove(auditPath); err != nil {
		t.Fatal(err)
	}
	if err = validateAdmission(repo, p, auditPath, true); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	repo, err = sqlite.New(filepath.Join(dir, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	audit, err = sqlite.NewAudit(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	e, err := application.NewEngine(repo, p, audit, func(a application.Actor) application.Actor { return a })
	if err != nil {
		t.Fatal(err)
	}
	health, err := e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "health", Payload: json.RawMessage(`{}`)}, application.Actor{Manage: true})
	if err != nil || !health.(v1.HealthResult).AuditDegraded {
		t.Fatal("lost audit gap hidden on restart")
	}
}

func TestManualDependencyFailureProducesOnlyFixedAlert(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	key := filepath.Join(root, "key")
	p, _ := protector.NewFixture(key)
	repo, err := sqlite.New(filepath.Join(root, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	audit, err := sqlite.NewAudit(filepath.Join(root, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := application.NewEngine(repo, p, audit, func(a application.Actor) application.Actor { return a })
	if err != nil {
		t.Fatal(err)
	}
	e.ConfigureOperations(&backup.Manager{Root: filepath.Join(root, "backups"), KeyFile: key, Repo: repo, Audit: audit})
	audit.Close()
	if _, err = e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "operations.retention", Payload: json.RawMessage(`{}`)}, application.Actor{Manage: true}); v1.SafeError(err).Error != v1.ErrInternal {
		t.Fatal("manual failure not exposed safely")
	}
	var event string
	var count uint64
	emitOperationsFailures(e, func(code string, n uint64) { event = code; count += n })
	if event != "operations_dependency_failed" || count != 1 {
		t.Fatal("safe manual alert missing")
	}
	emitOperationsFailures(e, func(string, uint64) { t.Fatal("duplicate alert") })
	health, err := e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "health", Payload: json.RawMessage(`{}`)}, application.Actor{Manage: true})
	if err != nil || !health.(v1.HealthResult).OperationsDegraded {
		t.Fatal("manual operation health healthy")
	}
}

func TestDailyScheduleSurvivesRestartDetectsInvalidAndSkipsClosed(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	repo, err := sqlite.New(filepath.Join(root, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	audit, err := sqlite.NewAudit(filepath.Join(root, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	key := filepath.Join(root, "key")
	p, err := protector.NewFixture(key)
	if err != nil {
		t.Fatal(err)
	}
	actor := application.Actor{Read: true, Manage: true}
	e, err := application.NewEngine(repo, p, audit, func(a application.Actor) application.Actor { return a })
	if err != nil {
		t.Fatal(err)
	}
	m := &backup.Manager{Root: filepath.Join(root, "backups"), KeyFile: key, Repo: repo, Audit: audit}
	e.ConfigureOperations(m)
	alerts := map[string]int{}
	alert := func(event string, count int) { alerts[event] += count }
	now := time.Now().UTC()
	next := maintain(ctx, e, m, actor, now, alert)
	if !next.After(now) || alerts["backup_overdue"] != 1 {
		t.Fatal("first due task missing")
	}
	entries, _ := os.ReadDir(m.Root)
	if len(entries) != 1 {
		t.Fatal("startup did not complete snapshot")
	}
	// Recreate the manager/engine: all scheduling evidence comes from disk.
	e, err = application.NewEngine(repo, p, audit, func(a application.Actor) application.Actor { return a })
	if err != nil {
		t.Fatal(err)
	}
	m = &backup.Manager{Root: m.Root, KeyFile: key, Repo: repo, Audit: audit}
	e.ConfigureOperations(m)
	again := maintain(ctx, e, m, actor, now.Add(time.Minute), alert)
	if !again.Equal(next) {
		t.Fatal("restart delayed persistent schedule")
	}
	stage := filepath.Join(m.Root, ".incomplete-concurrent")
	if err = os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	_, invalidStage, _, err := m.Schedule(ctx, now.Add(time.Minute))
	if err != nil || invalidStage != 0 {
		t.Fatal("unpublished staging counted as corrupt backup")
	}
	os.Remove(stage)
	// A published fresh backup must not hide failed retention on restart/retry.
	closedAudit := m.Audit
	closedAudit.Close()
	if retry := maintain(ctx, e, m, actor, now.Add(time.Minute), alert); !retry.Equal(now.Add(2*time.Minute)) || alerts["daily_operation_failed"] != 1 {
		t.Fatal("fresh backup hid retention failure")
	}
	m.Audit, err = sqlite.NewAudit(filepath.Join(root, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Audit.Close()
	if recovered := maintain(ctx, e, m, actor, now.Add(2*time.Minute), alert); !recovered.Equal(next) {
		t.Fatal("retention retry did not recover durable schedule")
	}
	due, invalid, overdue, err := m.Schedule(ctx, next.Add(time.Second))
	if err != nil || invalid != 0 || !overdue || !due.Equal(next) {
		t.Fatal("expiry not detected")
	}
	os.WriteFile(filepath.Join(m.Root, entries[0].Name(), "manifest.json"), []byte(`{}`), 0600)
	maintain(ctx, e, m, actor, now.Add(2*time.Minute), alert)
	if alerts["backup_invalid"] != 1 {
		t.Fatal("corruption invisible")
	}
	health, err := e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "health", Payload: json.RawMessage(`{}`)}, actor)
	if err != nil || !health.(v1.HealthResult).OperationsDegraded {
		t.Fatal("invalid backup health not degraded")
	}
	entries, _ = os.ReadDir(m.Root)
	if len(entries) != 2 {
		t.Fatal("bad snapshot erased or replacement missing")
	}
	s, _ := repo.Load(ctx)
	s.Revision++
	s.RecoveryClosed = true
	repo.Save(ctx, s)
	e, _ = application.NewEngine(repo, p, audit, func(a application.Actor) application.Actor { return a })
	e.ConfigureOperations(m)
	maintain(ctx, e, m, actor, next.Add(time.Hour), alert)
	after, _ := os.ReadDir(m.Root)
	if len(after) != len(entries) {
		t.Fatal("restored closed instance scheduled backup")
	}
}
