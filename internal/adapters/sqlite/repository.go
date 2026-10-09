package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/third-party-api-key-hub/internal/application"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

type Repository struct{ db *sql.DB }

func open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA foreign_keys=ON; PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func New(path string) (*Repository, error) {
	db, err := open(path)
	if err != nil {
		return nil, err
	}
	r := &Repository{db: db}
	if err = r.initialize(); err != nil {
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
	if _, err = tx.Exec("CREATE TABLE IF NOT EXISTS metadata(version INTEGER NOT NULL, revision INTEGER NOT NULL, operation_seq INTEGER NOT NULL)"); err != nil {
		return err
	}
	var version int
	err = tx.QueryRow("SELECT version FROM metadata").Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.Exec("INSERT INTO metadata VALUES(1,0,0)")
		version = 1
	}
	if err != nil {
		return err
	}
	if version != 1 {
		return errors.New("unsupported schema")
	}
	for _, table := range []string{"apps", "shops", "brands", "internal_shops", "installations", "credentials"} {
		if _, err = tx.Exec("CREATE TABLE IF NOT EXISTS " + table + "(id TEXT PRIMARY KEY, record TEXT NOT NULL)"); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *Repository) Load(ctx context.Context) (application.State, error) {
	var state application.State
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT revision,operation_seq FROM metadata").Scan(&state.Revision, &state.NextOperation); err != nil {
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
	if _, err = tx.ExecContext(ctx, "UPDATE metadata SET revision=?,operation_seq=?", state.Revision, state.NextOperation); err != nil {
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
	db, err := open(path)
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec("CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT, record TEXT NOT NULL)"); err != nil {
		db.Close()
		return nil, err
	}
	return &Audit{db: db}, nil
}
func (a *Audit) Record(ctx context.Context, event application.AuditEvent) error {
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
		var event application.AuditEvent
		if err = json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, errors.New("invalid audit record")
		}
		result = append(result, event)
	}
	return result, rows.Err()
}
func (a *Audit) Close() error { return a.db.Close() }
