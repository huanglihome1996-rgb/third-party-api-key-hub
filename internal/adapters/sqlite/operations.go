package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/third-party-api-key-hub/internal/application"
	"github.com/third-party-api-key-hub/internal/domain"
	"net/url"
	"os"
	"strings"
	"time"
)

const SchemaVersion = 2

func (r *Repository) RecordAuditGap(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, "UPDATE metadata SET audit_gap=1")
	return err
}
func snapshot(ctx context.Context, db *sql.DB, path string) error {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("snapshot target exists")
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(path, "'", "''")+"'"); err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func (r *Repository) Snapshot(ctx context.Context, path string) error {
	return snapshot(ctx, r.db, path)
}
func (a *Audit) Snapshot(ctx context.Context, path string) error { return snapshot(ctx, a.db, path) }

// A year means the older of calendar anniversary and 366 complete UTC days.
// This preserves at least a full year at leap-day/calendar normalization edges.
func RetentionCutoff(now time.Time) time.Time {
	now = now.UTC()
	calendar := now.AddDate(-1, 0, 0)
	elapsed := now.Add(-366 * 24 * time.Hour)
	if elapsed.Before(calendar) {
		return elapsed
	}
	return calendar
}
func (a *Audit) Retain(ctx context.Context, now time.Time) (int64, time.Time, error) {
	return a.RetainAuthorized(ctx, now, func() bool { return true })
}
func (a *Audit) RetainAuthorized(ctx context.Context, now time.Time, allowed func() bool) (int64, time.Time, error) {
	now = now.UTC()
	cutoff := RetentionCutoff(now)
	if now.IsZero() || now.Year() < 1970 {
		return 0, cutoff, errors.New("invalid retention time")
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, cutoff, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS retention_clock(id INTEGER PRIMARY KEY CHECK(id=1), last_time TEXT NOT NULL)"); err != nil {
		return 0, cutoff, err
	}
	var last string
	err = tx.QueryRowContext(ctx, "SELECT last_time FROM retention_clock WHERE id=1").Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, cutoff, err
	}
	if last != "" {
		at, err := time.Parse(time.RFC3339Nano, last)
		if err != nil || now.Before(at) {
			return 0, cutoff, errors.New("invalid retention clock")
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,record FROM events")
	if err != nil {
		return 0, cutoff, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		var raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, cutoff, err
		}
		event, recordErr := decodeAuditEvent(raw)
		if recordErr != nil || event.Time.After(now) {
			rows.Close()
			return 0, cutoff, errors.New("invalid audit clock or record")
		}
		if event.Time.Before(cutoff) {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, cutoff, err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, "DELETE FROM events WHERE id=?", id); err != nil {
			return 0, cutoff, err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO retention_clock(id,last_time) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET last_time=excluded.last_time", now.Format(time.RFC3339Nano)); err != nil {
		return 0, cutoff, err
	}
	if !allowed() {
		return 0, cutoff, domain.ErrPermission
	}
	if err = tx.Commit(); err != nil {
		return 0, cutoff, err
	}
	return int64(len(ids)), cutoff, nil
}

// Inspect opens an existing immutable snapshot read-only: never initializes or
// migrates an untrusted backup while checking its integrity and schema.
func Inspect(ctx context.Context, path string, audit bool) (application.State, error) {
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}).String())
	if err != nil {
		return application.State{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	return inspectConnection(ctx, db, audit, false)
}

// Startup reuses the snapshot checks on a normal connection so SQLite can
// recover committed WAL and hot rollback journals. Only startup accepts v1.
func inspectConnection(ctx context.Context, db *sql.DB, audit, allowLegacy bool) (application.State, error) {
	var err error
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return application.State{}, errors.New("snapshot integrity rejected")
	}
	var forbidden int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type IN ('trigger','view')").Scan(&forbidden); err != nil || forbidden != 0 {
		return application.State{}, errors.New("snapshot schema rejected")
	}
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		return application.State{}, err
	}
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return application.State{}, err
		}
		names[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return application.State{}, err
	}
	if audit {
		for name := range names {
			if name != "events" && name != "sqlite_sequence" && name != "retention_clock" {
				return application.State{}, errors.New("audit schema rejected")
			}
		}
		if !names["events"] {
			return application.State{}, errors.New("audit schema missing")
		}
		if err = columns(db, "events", []string{"id:INTEGER:0:1", "record:TEXT:1:0"}); err != nil {
			return application.State{}, err
		}
		if names["retention_clock"] {
			if err = columns(db, "retention_clock", []string{"id:INTEGER:0:1", "last_time:TEXT:1:0"}); err != nil {
				return application.State{}, err
			}
		}
		if names["sqlite_sequence"] {
			if err = columns(db, "sqlite_sequence", []string{"name::0:0", "seq::0:0"}); err != nil {
				return application.State{}, err
			}
		}
		if err = schemaConstraints(db, names, SchemaVersion); err != nil {
			return application.State{}, err
		}
		if err = auditControlRecords(ctx, db, names); err != nil {
			return application.State{}, err
		}
		if err = validateAuditRecords(ctx, db); err != nil {
			return application.State{}, err
		}
		return application.State{}, nil
	}
	expected := []string{"metadata", "apps", "shops", "brands", "internal_shops", "installations", "credentials"}
	if len(names) != len(expected) {
		return application.State{}, errors.New("business schema rejected")
	}
	for _, name := range expected {
		if !names[name] {
			return application.State{}, errors.New("business schema rejected")
		}
	}
	var version, count int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*),version FROM metadata").Scan(&count, &version); err != nil || count != 1 || (version != SchemaVersion && !(allowLegacy && version == 1)) {
		return application.State{}, errors.New("schema version rejected")
	}
	for _, name := range expected {
		cols := []string{"id:TEXT:0:1", "record:TEXT:1:0"}
		if name == "metadata" {
			cols = []string{"version:INTEGER:1:0", "revision:INTEGER:1:0", "operation_seq:INTEGER:1:0", "recovery_closed:INTEGER:1:0", "audit_gap:INTEGER:1:0"}
			if version == 1 {
				cols = cols[:3]
			}
		}
		if err = columns(db, name, cols); err != nil {
			return application.State{}, err
		}
	}
	if err = schemaConstraints(db, names, version); err != nil {
		return application.State{}, err
	}
	invalid := "typeof(version)!='integer' OR typeof(revision)!='integer' OR revision<0 OR revision=9223372036854775807 OR typeof(operation_seq)!='integer' OR operation_seq<0"
	if version == SchemaVersion {
		invalid += " OR typeof(recovery_closed)!='integer' OR recovery_closed NOT IN (0,1) OR typeof(audit_gap)!='integer' OR audit_gap NOT IN (0,1)"
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metadata WHERE "+invalid).Scan(&count); err != nil || count != 0 {
		return application.State{}, errors.New("metadata record rejected")
	}
	return (&Repository{db: db}).load(ctx, version == 1)
}

// Admission needs to validate every event, but does not need to retain the
// entire decoded history in memory as the caller-facing List method does.
func validateAuditRecords(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT record FROM events ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return err
		}
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if _, err = decodeAuditEvent(raw); err != nil {
			return err
		}
	}
	return rows.Err()
}

func auditControlRecords(ctx context.Context, db *sql.DB, names map[string]bool) error {
	if names["retention_clock"] {
		rows, err := db.QueryContext(ctx, "SELECT id,last_time FROM retention_clock")
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var id int
			var raw string
			if err = rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return err
			}
			at, parseErr := time.Parse(time.RFC3339Nano, raw)
			count++
			if count > 1 || id != 1 || parseErr != nil || at.IsZero() || at.Year() < 1970 {
				rows.Close()
				return errors.New("audit retention record rejected")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	var count, maximum int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(MAX(id),0) FROM events WHERE typeof(id)!='integer' OR id<=0").Scan(&count, &maximum); err != nil || count != 0 {
		return errors.New("audit event id rejected")
	}
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM events").Scan(&maximum); err != nil {
		return err
	}
	if !names["sqlite_sequence"] {
		return errors.New("audit sequence missing")
	}
	rows, err := db.QueryContext(ctx, "SELECT name,seq,typeof(seq) FROM sqlite_sequence")
	if err != nil {
		return err
	}
	defer rows.Close()
	count = 0
	for rows.Next() {
		var name, typ string
		var sequence int64
		if err = rows.Scan(&name, &sequence, &typ); err != nil {
			return err
		}
		count++
		if count > 1 || name != "events" || typ != "integer" || sequence < maximum || sequence < 0 || sequence == 9223372036854775807 {
			return errors.New("audit sequence record rejected")
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if maximum > 0 && count != 1 {
		return errors.New("audit sequence record missing")
	}
	return nil
}

func columns(db *sql.DB, table string, expected []string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &typ, &notnull, &defaultValue, &pk); err != nil {
			return err
		}
		if i >= len(expected) || fmt.Sprintf("%s:%s:%d:%d", name, typ, notnull, pk) != expected[i] {
			return errors.New("snapshot columns rejected")
		}
		i++
	}
	if rows.Err() != nil || i != len(expected) {
		return errors.New("snapshot columns rejected")
	}
	return nil
}

// Fixed schema declarations reject added checks, foreign keys, collation,
// uniqueness, generated columns and explicit indexes as well as missing flags.
func schemaConstraints(db *sql.DB, names map[string]bool, version int) error {
	for name := range names {
		var actual string
		if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&actual); err != nil {
			return err
		}
		expected := "CREATE TABLE " + name + "(id TEXT PRIMARY KEY, record TEXT NOT NULL)"
		switch name {
		case "metadata":
			expected = "CREATE TABLE metadata(version INTEGER NOT NULL, revision INTEGER NOT NULL, operation_seq INTEGER NOT NULL, recovery_closed INTEGER NOT NULL DEFAULT 0, audit_gap INTEGER NOT NULL DEFAULT 0)"
			if version == 1 {
				expected = "CREATE TABLE metadata(version INTEGER NOT NULL, revision INTEGER NOT NULL, operation_seq INTEGER NOT NULL)"
			}
		case "events":
			expected = "CREATE TABLE events(id INTEGER PRIMARY KEY AUTOINCREMENT, record TEXT NOT NULL)"
		case "retention_clock":
			expected = "CREATE TABLE retention_clock(id INTEGER PRIMARY KEY CHECK(id=1), last_time TEXT NOT NULL)"
		case "sqlite_sequence":
			expected = "CREATE TABLE sqlite_sequence(name,seq)"
		}
		normalize := func(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), "") }
		if normalize(actual) != normalize(expected) {
			return errors.New("snapshot constraints rejected")
		}
	}
	rows, err := db.Query("SELECT name,tbl_name,sql FROM sqlite_master WHERE type='index'")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, table string
		var statement sql.NullString
		if err = rows.Scan(&name, &table, &statement); err != nil {
			return err
		}
		if statement.Valid || !names[table] || table == "metadata" || table == "events" || table == "retention_clock" || table == "sqlite_sequence" || name != "sqlite_autoindex_"+table+"_1" {
			return errors.New("snapshot index rejected")
		}
	}
	return rows.Err()
}
