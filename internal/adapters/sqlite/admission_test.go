package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestExistingDatabaseRejectedWithoutInitialization(t *testing.T) {
	for _, audit := range []bool{false, true} {
		kind := "business"
		if audit {
			kind = "audit"
		}
		for _, damage := range []string{"empty", "truncated", "missing_table", "extra_table", "trigger", "index", "record", "constraints", "missing_metadata", "empty_metadata"} {
			if audit && (damage == "missing_metadata" || damage == "empty_metadata") {
				continue
			}
			t.Run(kind+"/"+damage, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "database.sqlite")
				var db interface {
					Exec(string, ...any) (sql.Result, error)
					Close() error
				}
				if audit {
					a, err := NewAudit(path)
					if err != nil {
						t.Fatal(err)
					}
					db = a.db
				} else {
					r, err := New(path)
					if err != nil {
						t.Fatal(err)
					}
					db = r.db
				}
				table := "apps"
				if audit {
					table = "events"
				}
				statement := ""
				switch damage {
				case "missing_table":
					statement = "DROP TABLE " + table
				case "extra_table":
					statement = "CREATE TABLE extra(value TEXT)"
				case "trigger":
					statement = "CREATE TRIGGER extra AFTER INSERT ON " + table + " BEGIN DELETE FROM " + table + "; END"
				case "index":
					statement = "CREATE INDEX extra ON " + table + "(record)"
				case "record":
					if audit {
						statement = "INSERT INTO events(record) VALUES('broken')"
					} else {
						statement = "INSERT INTO apps VALUES('a','broken')"
					}
				case "constraints":
					if audit {
						statement = "DROP TABLE events; CREATE TABLE events(id INTEGER PRIMARY KEY, record TEXT NOT NULL)"
					} else {
						statement = "DROP TABLE apps; CREATE TABLE apps(id TEXT PRIMARY KEY, record TEXT NOT NULL CHECK(length(record)>0))"
					}
				case "missing_metadata":
					statement = "DROP TABLE metadata"
				case "empty_metadata":
					statement = "DELETE FROM metadata"
				}
				if statement != "" {
					if _, err := db.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				if damage == "empty" || damage == "truncated" {
					raw := []byte{}
					if damage == "truncated" {
						raw = []byte("SQLite format 3\x00")
					}
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if audit {
					a, err := NewAudit(path)
					if err == nil {
						a.Close()
						t.Error("damaged existing audit database admitted")
					}
				} else {
					r, err := New(path)
					if err == nil {
						r.Close()
						t.Error("damaged existing business database admitted")
					}
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Error("damaged existing database modified during rejection")
				}
			})
		}
	}
}

func TestCompleteLegacySchemaMigratesWithoutLosingCommittedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "business.sqlite")
	db, err := open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE metadata(version INTEGER NOT NULL, revision INTEGER NOT NULL, operation_seq INTEGER NOT NULL); INSERT INTO metadata VALUES(1,7,11)"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"apps", "shops", "brands", "internal_shops", "installations", "credentials"} {
		if _, err = db.Exec("CREATE TABLE " + table + "(id TEXT PRIMARY KEY, record TEXT NOT NULL)"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO apps VALUES('a','{"app_id":"a","platform":"fixture","provider_app_id":"pa","row_revision":7}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, err := r.Load(context.Background())
	if err != nil || s.Revision != 7 || s.NextOperation != 11 || len(s.Apps) != 1 || s.RecoveryClosed || s.AuditGap {
		t.Fatalf("legacy state changed: %+v, %v", s, err)
	}
}

func TestAuditAdmissionRejectsInvalidControlRecords(t *testing.T) {
	for _, statement := range []string{
		"CREATE TABLE retention_clock(id INTEGER PRIMARY KEY CHECK(id=1), last_time TEXT NOT NULL); INSERT INTO retention_clock VALUES(1,'broken')",
		"INSERT INTO sqlite_sequence VALUES('unexpected',1)",
		"INSERT INTO sqlite_sequence VALUES('events',-1)",
		"INSERT INTO sqlite_sequence VALUES('events',1),('events',2)",
		"INSERT INTO sqlite_sequence VALUES('events',9223372036854775807)",
	} {
		path := filepath.Join(t.TempDir(), "audit.sqlite")
		a, err := NewAudit(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		a.Close()
		before, _ := os.ReadFile(path)
		if a, err = NewAudit(path); err == nil {
			a.Close()
			t.Errorf("invalid audit control record admitted: %s", statement)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("invalid audit control records modified")
		}
		if _, err = Inspect(context.Background(), path, true); err == nil {
			t.Errorf("invalid audit control record accepted as snapshot: %s", statement)
		}
	}
}

func TestAdmissionRejectsInvalidMetadataWithoutMigration(t *testing.T) {
	for _, statement := range []string{
		"DELETE FROM metadata",
		"INSERT INTO metadata VALUES(2,0,0,0,0)",
		"UPDATE metadata SET version=999",
		"UPDATE metadata SET revision=-1",
		"UPDATE metadata SET operation_seq=-1",
		"UPDATE metadata SET recovery_closed=2",
		"UPDATE metadata SET audit_gap=-1",
		"UPDATE metadata SET recovery_closed='true'",
		"UPDATE metadata SET revision=9223372036854775807",
	} {
		path := filepath.Join(t.TempDir(), "business.sqlite")
		r, err := New(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		r.Close()
		before, _ := os.ReadFile(path)
		if r, err = New(path); err == nil {
			r.Close()
			t.Errorf("invalid metadata admitted: %s", statement)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("invalid metadata modified")
		}
	}
}

func TestInvalidLegacyDatabaseRejectedWithoutMigration(t *testing.T) {
	for _, statement := range []string{"DROP TABLE apps", "INSERT INTO apps VALUES('a','broken')", "UPDATE metadata SET revision=-1", "UPDATE metadata SET operation_seq=-1"} {
		path := filepath.Join(t.TempDir(), "business.sqlite")
		db, err := open(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("CREATE TABLE metadata(version INTEGER NOT NULL, revision INTEGER NOT NULL, operation_seq INTEGER NOT NULL); INSERT INTO metadata VALUES(1,7,11)"); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"apps", "shops", "brands", "internal_shops", "installations", "credentials"} {
			if _, err = db.Exec("CREATE TABLE " + table + "(id TEXT PRIMARY KEY, record TEXT NOT NULL)"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		db.Close()
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if r, err := New(path); err == nil {
			r.Close()
			t.Errorf("invalid v1 admitted: %s", statement)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("invalid v1 migrated or changed")
		}
	}
}

func TestPersistedAuditEventRequiresCompleteFields(t *testing.T) {
	base := map[string]any{"time": "2026-10-09T00:00:00Z", "uid": 0, "operation": "app.list", "target": "unknown", "result": "success", "operation_id": "op-test"}
	for _, damage := range []string{"empty", "time", "zero_time", "operation", "target", "result", "operation_id", "invalid_result"} {
		t.Run(damage, func(t *testing.T) {
			fields := map[string]any{}
			for k, v := range base {
				fields[k] = v
			}
			switch damage {
			case "empty":
				fields = map[string]any{}
			case "zero_time":
				fields["time"] = "0001-01-01T00:00:00Z"
			case "invalid_result":
				fields["result"] = "unknown"
			default:
				delete(fields, damage)
			}
			raw, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "audit.sqlite")
			a, err := NewAudit(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = a.db.Exec("INSERT INTO events(record) VALUES(?)", string(raw)); err != nil {
				t.Fatal(err)
			}
			if _, err = a.List(context.Background()); err == nil {
				t.Error("incomplete persisted event listed")
			}
			if _, _, err = a.Retain(context.Background(), time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)); err == nil {
				t.Error("incomplete persisted event retained")
			}
			a.Close()
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if a, err = NewAudit(path); err == nil {
				a.Close()
				t.Error("incomplete persisted event admitted")
			}
			if _, err = Inspect(context.Background(), path, true); err == nil {
				t.Error("incomplete event accepted in snapshot")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("incomplete event modified")
			}
		})
	}
}

func TestUnsafeRecoverySidecarsRejectedBeforeOpeningDatabase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions")
	}
	for _, existing := range []bool{false, true} {
		for _, audit := range []bool{false, true} {
			for _, suffix := range []string{"-wal", "-shm", "-journal"} {
				for _, kind := range []string{"permissions", "symlink", "directory"} {
					t.Run(fmt.Sprintf("existing=%v/audit=%v/%s/%s", existing, audit, suffix, kind), func(t *testing.T) {
						path := filepath.Join(t.TempDir(), "database.sqlite")
						if existing {
							if audit {
								a, err := NewAudit(path)
								if err != nil {
									t.Fatal(err)
								}
								a.Close()
							} else {
								r, err := New(path)
								if err != nil {
									t.Fatal(err)
								}
								r.Close()
							}
						}
						before, _ := os.ReadFile(path)
						sidecar := path + suffix
						switch kind {
						case "permissions":
							if err := os.WriteFile(sidecar, []byte("fixture-sidecar"), 0644); err != nil {
								t.Fatal(err)
							}
						case "directory":
							if err := os.Mkdir(sidecar, 0700); err != nil {
								t.Fatal(err)
							}
						case "symlink":
							target := filepath.Join(filepath.Dir(path), "target")
							if err := os.WriteFile(target, []byte("fixture-target"), 0600); err != nil {
								t.Fatal(err)
							}
							if err := os.Symlink(target, sidecar); err != nil {
								t.Fatal(err)
							}
						}
						if audit {
							a, err := NewAudit(path)
							if err == nil {
								a.Close()
								t.Error("unsafe audit sidecar admitted")
							}
						} else {
							r, err := New(path)
							if err == nil {
								r.Close()
								t.Error("unsafe business sidecar admitted")
							}
						}
						if !existing {
							if _, err := os.Lstat(path); !os.IsNotExist(err) {
								t.Error("new database created beside orphan recovery file")
							}
						} else {
							after, err := os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
							if !bytes.Equal(before, after) {
								t.Error("database changed before sidecar admission")
							}
						}
						info, err := os.Lstat(sidecar)
						if err != nil {
							t.Fatal("unsafe sidecar removed", err)
						}
						if kind == "permissions" && info.Mode().Perm() != 0644 {
							t.Error("unsafe sidecar permissions changed")
						}
					})
				}
			}
		}
	}
}

func TestAdmissionRejectsUnsafeExistingPermissionsAndSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions")
	}
	for _, audit := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "database.sqlite")
		if audit {
			a, err := NewAudit(path)
			if err != nil {
				t.Fatal(err)
			}
			a.Close()
		} else {
			r, err := New(path)
			if err != nil {
				t.Fatal(err)
			}
			r.Close()
		}
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if audit {
			a, err := NewAudit(path)
			if err == nil {
				a.Close()
				t.Error("unsafe audit permissions admitted")
			}
		} else {
			r, err := New(path)
			if err == nil {
				r.Close()
				t.Error("unsafe business permissions admitted")
			}
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0644 {
			t.Fatal("existing permissions silently repaired")
		}
		os.Chmod(path, 0600)
		link := filepath.Join(filepath.Dir(path), "link.sqlite")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		if audit {
			a, err := NewAudit(link)
			if err == nil {
				a.Close()
				t.Error("audit symlink admitted")
			}
		} else {
			r, err := New(link)
			if err == nil {
				r.Close()
				t.Error("business symlink admitted")
			}
		}
	}
}
