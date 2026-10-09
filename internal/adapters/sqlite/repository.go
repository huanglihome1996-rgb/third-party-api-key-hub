package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/third-party-api-key-hub/internal/application"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

type Repository struct{ db *sql.DB }

// openDatabase distinguishes an exclusively created file from existing storage.
// Existing files are opened without CREATE or journaling changes: the first
// read may recover a legitimate hot journal/WAL, but must never build schema.
func openDatabase(path string) (*sql.DB, bool, error) {
	info, err := os.Lstat(path)
	existing := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	if existing && !safeStorageMode(info) {
		return nil, false, errors.New("unsafe database permissions or type")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if !existing || !safeStorageMode(info) {
			return nil, false, errors.New("unsafe or orphan database recovery file")
		}
	}
	if existing {
		if err := validateRecoveryFiles(path); err != nil {
			return nil, false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	created := err == nil
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, false, err
	}
	if created {
		if err = f.Close(); err != nil {
			return nil, true, err
		}
	} else {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, false, err
		}
		if !safeStorageMode(info) {
			return nil, false, errors.New("unsafe database permissions or type")
		}
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}).String())
	if err != nil {
		return nil, created, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, created, err
	}
	return db, created, nil
}

func safeStorageMode(info os.FileInfo) bool {
	return info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode().Perm()&0077 == 0)
}

func configureDurability(db *sql.DB) error {
	_, err := db.Exec("PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL;")
	return err
}

func New(path string) (*Repository, error) {
	return NewAdmitted(path, nil)
}

// NewAdmitted validates existing semantic state and key binding before changing
// durability settings or migrating a valid old schema. New retains the common
// structural checks for internal callers without deployment-specific policy.
func NewAdmitted(path string, admit func(application.State) error) (*Repository, error) {
	db, created, err := openDatabase(path)
	if err != nil {
		return nil, err
	}
	r := &Repository{db: db}
	if !created {
		var state application.State
		if state, err = inspectConnection(context.Background(), db, false, true); err == nil && admit != nil {
			err = admit(state)
		}
		if err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = configureDurability(db); err != nil {
		db.Close()
		return nil, err
	}
	if created {
		err = r.initialize()
	} else {
		var version int
		err = db.QueryRow("SELECT version FROM metadata").Scan(&version)
		if err == nil && version == 1 {
			err = r.migrateLegacy()
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}
func (r *Repository) initialize() error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("CREATE TABLE metadata(version INTEGER NOT NULL, revision INTEGER NOT NULL, operation_seq INTEGER NOT NULL, recovery_closed INTEGER NOT NULL DEFAULT 0, audit_gap INTEGER NOT NULL DEFAULT 0)"); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO metadata VALUES(2,0,0,0,0)"); err != nil {
		return err
	}
	for _, table := range []string{"apps", "shops", "brands", "internal_shops", "installations", "credentials"} {
		if _, err = tx.Exec("CREATE TABLE " + table + "(id TEXT PRIMARY KEY, record TEXT NOT NULL)"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Only a completely validated v1 database reaches this migration. No tables
// or metadata rows are created while upgrading existing business storage.
func (r *Repository) migrateLegacy() error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{"ALTER TABLE metadata ADD COLUMN recovery_closed INTEGER NOT NULL DEFAULT 0", "ALTER TABLE metadata ADD COLUMN audit_gap INTEGER NOT NULL DEFAULT 0", "UPDATE metadata SET version=2"} {
		if _, err = tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *Repository) Load(ctx context.Context) (application.State, error) {
	return r.load(ctx, false)
}
func (r *Repository) load(ctx context.Context, legacy bool) (application.State, error) {
	var state application.State
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	query := "SELECT revision,operation_seq,recovery_closed,audit_gap FROM metadata"
	if legacy {
		query = "SELECT revision,operation_seq,0,0 FROM metadata"
	}
	if err := tx.QueryRowContext(ctx, query).Scan(&state.Revision, &state.NextOperation, &state.RecoveryClosed, &state.AuditGap); err != nil {
		return state, err
	}
	for table, dst := range map[string]any{"apps": &state.Apps, "shops": &state.Shops, "brands": &state.Brands, "internal_shops": &state.Internals, "installations": &state.Installations, "credentials": &state.Credentials} {
		rows, err := tx.QueryContext(ctx, "SELECT id,record FROM "+table)
		if err != nil {
			return state, err
		}
		values := map[string]json.RawMessage{}
		for rows.Next() {
			var id, record string
			if err = rows.Scan(&id, &record); err != nil {
				rows.Close()
				return state, err
			}
			// Reject unknown fields, aliases, duplicate fields and malformed
			// Unicode inside persisted records instead of silently normalizing them.
			value := reflect.New(reflect.TypeOf(dst).Elem().Elem()).Interface()
			if err = v1.DecodeStrict(bytes.NewReader([]byte(record)), value); err != nil {
				rows.Close()
				return state, errors.New("invalid persisted record")
			}
			values[id] = json.RawMessage(record)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return state, err
		}
		raw, err := json.Marshal(values)
		if err != nil {
			return state, err
		}
		if err = json.Unmarshal(raw, dst); err != nil {
			return state, err
		}
	}
	return state, tx.Commit()
}
func (r *Repository) Save(ctx context.Context, state application.State) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM metadata").Scan(&current); err != nil {
		return err
	}
	if state.Revision != current+1 {
		return errors.New("repository revision conflict")
	}
	for table, src := range map[string]any{"apps": state.Apps, "shops": state.Shops, "brands": state.Brands, "internal_shops": state.Internals, "installations": state.Installations, "credentials": state.Credentials} {
		raw, err := json.Marshal(src)
		if err != nil {
			return err
		}
		values := map[string]json.RawMessage{}
		if err = json.Unmarshal(raw, &values); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
		for id, record := range values {
			if _, err = tx.ExecContext(ctx, "INSERT INTO "+table+"(id,record) VALUES(?,?)", id, string(record)); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE metadata SET revision=?,operation_seq=?,recovery_closed=?,audit_gap=MAX(audit_gap,?)", state.Revision, state.NextOperation, state.RecoveryClosed, state.AuditGap); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repository) Close() error { return r.db.Close() }

// Audit is separate from the business transaction and never receives payloads.
type Audit struct {
	db *sql.DB
}

func NewAudit(path string) (*Audit, error) {
	db, created, err := openDatabase(path)
	if err != nil {
		return nil, err
	}
	if !created {
		if _, err = inspectConnection(context.Background(), db, true, false); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = configureDurability(db); err != nil {
		db.Close()
		return nil, err
	}
	if created {
		_, err = db.Exec("CREATE TABLE events(id INTEGER PRIMARY KEY AUTOINCREMENT, record TEXT NOT NULL)")
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Audit{db: db}, nil
}
func (a *Audit) Record(ctx context.Context, event application.AuditEvent) error {
	if err := validateAuditEvent(event); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, "INSERT INTO events(record) VALUES(?)", string(raw))
	return err
}
func (a *Audit) List(ctx context.Context) ([]application.AuditEvent, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT record FROM events ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []application.AuditEvent{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		event, err := decodeAuditEvent(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func decodeAuditEvent(raw string) (application.AuditEvent, error) {
	var event application.AuditEvent
	if err := v1.DecodeStrict(bytes.NewReader([]byte(raw)), &event); err != nil {
		return event, errors.New("invalid audit record")
	}
	return event, validateAuditEvent(event)
}

func validateAuditEvent(event application.AuditEvent) error {
	if event.Time.IsZero() || strings.TrimSpace(event.Operation) == "" || strings.TrimSpace(event.Target) == "" || strings.TrimSpace(event.OperationID) == "" || (event.Result != "success" && event.Result != "failure") {
		return errors.New("incomplete or invalid audit record")
	}
	return nil
}
func (a *Audit) Close() error { return a.db.Close() }
