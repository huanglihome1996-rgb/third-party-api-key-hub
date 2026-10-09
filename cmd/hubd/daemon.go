package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/third-party-api-key-hub/internal/adapters/backup"
	"github.com/third-party-api-key-hub/internal/adapters/local"
	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/adapters/sqlite"
	"github.com/third-party-api-key-hub/internal/application"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "event=startup_or_runtime_failed count=1")
		os.Exit(1)
	}
}
func run() error {
	fixture := flag.Bool("fixture", false, "run isolated fictional local demonstration only")
	dir := flag.String("state-dir", "/tmp/api-key-hub-fixture", "isolated fixture state directory")
	restore := flag.String("restore-from", "", "offline restore of a validated fixture backup into a NEW state directory")
	keyFile := flag.String("fixture-key-file", "", "existing independent fixture key; never generated when specified")
	flag.Parse()
	explicitKey := *keyFile != ""
	if flag.NArg() != 0 || !*fixture || runtime.GOOS != "linux" {
		return fmt.Errorf("production deployment gated")
	}
	absolute, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	*dir = absolute
	if err = backup.SafePath(*dir); err != nil {
		return err
	}
	if *restore != "" && *keyFile == "" {
		return fmt.Errorf("independent recovery key required")
	}
	// A stable sibling lock is shared by normal startup and offline restore,
	// before touching the state directory, databases, key material, or socket.
	release, err := local.AcquireInstanceLock(*dir + ".lock")
	if err != nil {
		return err
	}
	defer release()
	if *restore != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err = backup.Restore(ctx, *restore, *dir, *keyFile); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "event=restore_closed count=1")
		return nil
	}
	if err = os.MkdirAll(*dir, 0700); err != nil {
		return err
	}
	if err = os.Chmod(*dir, 0700); err != nil {
		return err
	}
	business := filepath.Join(*dir, "business.sqlite")
	auditPath := filepath.Join(*dir, "audit.sqlite")
	if backup.SafePath(business) != nil || backup.SafePath(auditPath) != nil {
		return fmt.Errorf("unsafe storage")
	}
	_, statErr := os.Lstat(business)
	existing := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if *keyFile == "" {
		*keyFile = filepath.Join(*dir, "fixture.key")
	}
	if err = backup.SafePath(*keyFile); err != nil {
		return err
	}
	var p *protector.Fixture
	// Existing databases always require existing material: losing a key must not
	// silently create a new incompatible key, even for a fixture instance.
	if existing || explicitKey {
		p, err = protector.NewExistingFixture(*keyFile)
	} else {
		p, err = protector.NewFixture(*keyFile)
	}
	if err != nil {
		return err
	}
	repo, err := sqlite.New(business)
	if err != nil {
		return err
	}
	defer repo.Close()
	if err = validateAdmission(repo, p, auditPath, existing); err != nil {
		return err
	}
	audit, err := sqlite.NewAudit(auditPath)
	if err != nil {
		return err
	}
	defer audit.Close()
	uid := uint32(os.Getuid())
	authorize := func(a application.Actor) application.Actor { a.Read = a.UID == uid; a.Manage = a.UID == uid; return a }
	engine, err := application.NewEngine(repo, p, audit, authorize)
	if err != nil {
		return err
	}
	operations := &backup.Manager{Root: filepath.Join(*dir, "backups"), KeyFile: *keyFile, Repo: repo, Audit: audit}
	engine.ConfigureOperations(operations)
	listener, err := local.Listen(filepath.Join(*dir, "hub.sock"))
	if err != nil {
		return err
	}
	defer listener.Close()
	server := local.Server(engine, authorize)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		shutdownDone <- server.Shutdown(shutdown)
	}()
	jobsDone := make(chan struct{})
	go func() {
		defer close(jobsDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var next time.Time // Startup immediately checks durable schedule/overdue packages.
		reported := false
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				emitOperationsFailures(engine, func(event string, count uint64) { fmt.Fprintf(os.Stderr, "event=%s count=%d\n", event, count) })
				if engine.AuditDegraded() && !reported {
					fmt.Fprintln(os.Stderr, "event=audit_degraded count=1")
					reported = true
				}
				if !now.Before(next) {
					job, cancel := context.WithTimeout(ctx, 2*time.Minute)
					actor := application.Actor{UID: uid, Name: "daily_fixture"}
					next = maintain(job, engine, operations, actor, now.UTC(), func(event string, count int) { fmt.Fprintf(os.Stderr, "event=%s count=%d\n", event, count) })
					cancel()
				}
			}
		}
	}()
	fmt.Fprintln(os.Stderr, "event=fixture_ready count=1")
	err = server.Serve(listener)
	// Serve returns as soon as listeners close; Shutdown and maintenance must
	// finish before deferred database Close executes.
	stop()
	shutdownErr := <-shutdownDone
	<-jobsDone
	if shutdownErr != nil {
		server.Close()
		engine.Drain()
		return shutdownErr
	}
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func validateAdmission(repo *sqlite.Repository, p application.Protector, auditPath string, existing bool) error {
	state, err := repo.Load(context.Background())
	if err != nil {
		return err
	}
	if err = application.ValidateRestoreState(state, p, application.FixtureProfiles()); err != nil {
		return err
	}
	if existing {
		if _, err = os.Lstat(auditPath); os.IsNotExist(err) {
			return repo.RecordAuditGap(context.Background())
		} else if err != nil {
			return err
		}
	}
	return nil
}

func emitOperationsFailures(e *application.Engine, alert func(string, uint64)) {
	if count := e.DrainOperationsFailures(); count > 0 {
		alert("operations_dependency_failed", count)
	}
}

// maintain accepts an injected time so restart/expiry tests require no real-day
// waits. Its alert vocabulary is fixed; no paths, errors or input are emitted.
func maintain(ctx context.Context, e *application.Engine, m *backup.Manager, actor application.Actor, now time.Time, alert func(string, int)) time.Time {
	retry := now.Add(time.Minute)
	status, err := e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "recovery.status", Payload: json.RawMessage(`{}`)}, actor)
	if err != nil {
		e.SetOperationsDegraded(true)
		alert("daily_operation_failed", 1)
		return retry
	}
	if status.(v1.RecoveryStatusResult).RecoveryClosed {
		return retry
	}
	next, invalid, overdue, err := m.Schedule(ctx, now)
	if err != nil {
		e.SetOperationsDegraded(true)
		alert("backup_invalid", 1)
		return retry
	}
	if invalid > 0 {
		alert("backup_invalid", invalid)
	}
	e.SetOperationsDegraded(invalid > 0 || overdue)
	if overdue {
		alert("backup_overdue", 1)
		result, err := e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "operations.backup", Payload: json.RawMessage(`{}`)}, actor)
		if err != nil {
			e.SetOperationsDegraded(true)
			alert("daily_operation_failed", 1)
			return retry
		}
		at, err := time.Parse(time.RFC3339Nano, result.(v1.BackupResult).CreatedAt)
		if err != nil {
			e.SetOperationsDegraded(true)
			alert("daily_operation_failed", 1)
			return retry
		}
		next = at.Add(24 * time.Hour)
	}
	// A fresh backup does not prove retention succeeded. Retry retention even
	// when a preceding attempt published its backup before cleanup failed.
	_, err = e.Execute(ctx, v1.OperationRequest{Version: v1.Version, Operation: "operations.retention", Payload: json.RawMessage(`{}`)}, actor)
	if err != nil {
		e.SetOperationsDegraded(true)
		alert("daily_operation_failed", 1)
		return retry
	}
	e.SetOperationsDegraded(invalid > 0)
	return next
}
