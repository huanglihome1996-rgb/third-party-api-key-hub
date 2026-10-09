package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/third-party-api-key-hub/internal/application"
	"github.com/third-party-api-key-hub/internal/domain"
	"path/filepath"
	"testing"
	"time"
)

func fixtureAuditEvent(at time.Time) application.AuditEvent {
	return application.AuditEvent{Time: at, Operation: "app.list", Target: "unknown", Result: "success", OperationID: "op-test"}
}

func TestAuditAdmissionRejectsCorruptionAfterLongValidPrefix(t *testing.T) {
	for _, damage := range []string{"{}", `{"time":"2026-10-09T00:00:00Z","operation":"app.list","target":"unknown","result":"success","operation_id":"op-test","unknown":true}`} {
		path := filepath.Join(t.TempDir(), "audit.sqlite")
		a, err := NewAudit(path)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(fixtureAuditEvent(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.db.Exec("WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<512) INSERT INTO events(record) SELECT ? FROM n", string(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err = a.db.Exec("INSERT INTO events(record) VALUES(?)", damage); err != nil {
			t.Fatal(err)
		}
		if _, err = a.List(context.Background()); err == nil {
			t.Fatal("late corrupted audit event listed")
		}
		a.Close()
		if _, err = Inspect(context.Background(), path, true); err == nil {
			t.Fatal("late corrupted audit event admitted in snapshot")
		}
		if a, err = NewAudit(path); err == nil {
			a.Close()
			t.Fatal("late corrupted audit event admitted at startup")
		}
	}
}

func TestAuditRecordValidationCancellationReleasesConnection(t *testing.T) {
	a, err := NewAudit(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	raw, err := json.Marshal(fixtureAuditEvent(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<20000) INSERT INTO events(record) SELECT ? FROM n", string(raw)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err = validateAuditRecords(ctx, a.db); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("audit scan ignored deadline: %v", err)
	}
	// With a single connection, a leaked cursor would block the next operation.
	checkCtx, checkCancel := context.WithTimeout(context.Background(), time.Second)
	defer checkCancel()
	var count int
	if err = a.db.QueryRowContext(checkCtx, "SELECT COUNT(*) FROM events").Scan(&count); err != nil || count != 20000 {
		t.Fatalf("canceled scan leaked connection or changed records: count=%d err=%v", count, err)
	}
}

func TestRetentionUTCStrictBoundaryAndFailClosed(t *testing.T) {
	a, err := NewAudit(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	now := time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC)
	cutoff := RetentionCutoff(now)
	for _, at := range []time.Time{cutoff.Add(-time.Nanosecond), cutoff, cutoff.Add(time.Nanosecond)} {
		if err = a.Record(context.Background(), fixtureAuditEvent(at)); err != nil {
			t.Fatal(err)
		}
	}
	n, _, err := a.Retain(context.Background(), now)
	if err != nil || n != 1 {
		t.Fatalf("removed %d error %v", n, err)
	}
	events, _ := a.List(context.Background())
	if len(events) != 2 {
		t.Fatal("boundary removed")
	}
	if _, _, err = a.Retain(context.Background(), now.Add(-time.Second)); err == nil {
		t.Fatal("rollback accepted")
	}
	a.Record(context.Background(), fixtureAuditEvent(now.Add(time.Hour)))
	if _, _, err = a.Retain(context.Background(), now); err == nil {
		t.Fatal("future accepted")
	}
	a.db.Exec("INSERT INTO events(record) VALUES('broken')")
	if _, _, err = a.Retain(context.Background(), now.Add(2*time.Hour)); err == nil {
		t.Fatal("corruption accepted")
	}
}

func TestRetentionIndependentCalendarUTCExamplesAndYoungEvent(t *testing.T) {
	for _, test := range []struct{ now, expected string }{
		{"2024-02-29T12:00:00Z", "2023-02-28T12:00:00Z"},
		{"2025-03-01T00:00:00Z", "2024-02-29T00:00:00Z"},
		{"2026-10-09T08:30:00+08:00", "2025-10-08T00:30:00Z"},
		{"2024-03-01T00:00:00-05:00", "2023-03-01T05:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, test.now)
		expected, _ := time.Parse(time.RFC3339, test.expected)
		if cutoff := RetentionCutoff(now); !cutoff.Equal(expected) || cutoff.Location() != time.UTC {
			t.Fatalf("cutoff %s expected %s", cutoff, test.expected)
		}
	}
	a, err := NewAudit(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	// Independent dates: a 30-day retention regression must not delete June's event.
	for _, at := range []time.Time{time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)} {
		if err = a.Record(context.Background(), fixtureAuditEvent(at)); err != nil {
			t.Fatal(err)
		}
	}
	removed, _, err := a.Retain(context.Background(), now)
	if err != nil || removed != 1 {
		t.Fatal("incorrect independent history retention", err)
	}
	events, err := a.List(context.Background())
	if err != nil || len(events) != 1 || !events[0].Time.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("younger than one year event removed")
	}
}

func TestSnapshotRejectsMissingConstraintsAndUnsupportedIndexes(t *testing.T) {
	for _, statement := range []string{
		"CREATE TABLE events(id INTEGER, record TEXT NOT NULL)",
		"CREATE TABLE events(id INTEGER PRIMARY KEY AUTOINCREMENT, record TEXT)",
		"CREATE TABLE events(id INTEGER PRIMARY KEY AUTOINCREMENT, record TEXT NOT NULL CHECK(length(record)>0))",
		"CREATE TABLE events(id INTEGER PRIMARY KEY AUTOINCREMENT, record TEXT NOT NULL); CREATE INDEX unsafe_index ON events(record)",
	} {
		path := filepath.Join(t.TempDir(), "malformed.sqlite")
		db, err := open(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if _, err = Inspect(context.Background(), path, true); err == nil {
			t.Fatalf("malformed schema admitted: %s", statement)
		}
	}
	path := filepath.Join(t.TempDir(), "duplicate-business.sqlite")
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.db.Exec("DROP TABLE apps; CREATE TABLE apps(id TEXT,record TEXT NOT NULL); INSERT INTO apps VALUES('duplicate','{}'),('duplicate','{}')"); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if _, err = Inspect(context.Background(), path, false); err == nil {
		t.Fatal("duplicate business IDs admitted")
	}
}

func TestSnapshotRejectsExtraSchemaAndDoesNotOverwriteTarget(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	r, err := New(filepath.Join(root, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	snapshotPath := filepath.Join(root, "snapshot.sqlite")
	if err = r.Snapshot(ctx, snapshotPath); err != nil {
		t.Fatal(err)
	}
	if err = r.Snapshot(ctx, snapshotPath); err == nil {
		t.Fatal("snapshot overwritten")
	}
	db, err := open(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("ALTER TABLE metadata ADD COLUMN extra TEXT"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err = Inspect(ctx, snapshotPath, false); err == nil {
		t.Fatal("unexpected column accepted")
	}
	second := filepath.Join(root, "with-view.sqlite")
	r.Snapshot(ctx, second)
	db, _ = open(second)
	db.Exec("CREATE VIEW extra AS SELECT revision FROM metadata")
	db.Close()
	if _, err = Inspect(ctx, second, false); err == nil {
		t.Fatal("view accepted")
	}
}

func TestStrictPersistedRecordPreservesExpiryAndRejectsUnknownFields(t *testing.T) {
	r, err := New(filepath.Join(t.TempDir(), "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	expiry := time.Now().UTC().Add(time.Hour)
	s := application.EmptyState()
	s.Revision = 1
	s.Credentials["app|a||client_secret"] = domain.Credential{Scope: "app", AppID: "a", Type: "client_secret", Generation: 1, RowRevision: 1, ExpiryKnown: true, ExpiresAt: &expiry}
	if err = r.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	actual, err := r.Load(context.Background())
	if err != nil || !actual.Credentials["app|a||client_secret"].ExpiresAt.Equal(expiry) {
		t.Fatal("expiry lost", err)
	}
	r.db.Exec(`UPDATE credentials SET record=json_set(record,'$.unknown','fake-hidden')`)
	if _, err = r.Load(context.Background()); err == nil {
		t.Fatal("unknown persisted field accepted")
	}
}

func TestActualV1MigrationRecoveryAndGapRemainAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "business.sqlite")
	db, err := open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE metadata(version INTEGER NOT NULL, revision INTEGER NOT NULL, operation_seq INTEGER NOT NULL); INSERT INTO metadata VALUES(1,0,0)")
	if err == nil {
		for _, table := range []string{"apps", "shops", "brands", "internal_shops", "installations", "credentials"} {
			if _, err = db.Exec("CREATE TABLE " + table + "(id TEXT PRIMARY KEY, record TEXT NOT NULL)"); err != nil {
				break
			}
		}
	}
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Load(context.Background())
	if err != nil || s.RecoveryClosed || s.AuditGap {
		t.Fatal("migration state", err)
	}
	s.Revision++
	s.RecoveryClosed = true
	if err = r.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if err = r.RecordAuditGap(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A stale in-memory gap=false must never erase durable gap evidence.
	s.Revision++
	if err = r.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	r.Close()
	r, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, err = r.Load(context.Background())
	if err != nil || !s.RecoveryClosed || !s.AuditGap {
		t.Fatal("durability lost", err)
	}
}

func TestRetentionPermissionRevocationRollsBack(t *testing.T) {
	a, err := NewAudit(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	now := time.Now().UTC()
	a.Record(context.Background(), fixtureAuditEvent(now.AddDate(-2, 0, 0)))
	if _, _, err = a.RetainAuthorized(context.Background(), now, func() bool { return false }); err == nil {
		t.Fatal("revocation committed")
	}
	events, err := a.List(context.Background())
	if err != nil || len(events) != 1 {
		t.Fatal("events deleted after revocation")
	}
	var n int
	err = a.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='retention_clock'").Scan(&n)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("clock persisted after rollback")
	}
}
