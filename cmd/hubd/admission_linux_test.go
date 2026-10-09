//go:build linux

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/adapters/sqlite"
	"github.com/third-party-api-key-hub/internal/application"
	"github.com/third-party-api-key-hub/internal/domain"
)

func TestDaemonAdmissionChild(t *testing.T) {
	if os.Getenv("HUB_ADMISSION_CHILD") != "1" {
		return
	}
	if mode := os.Getenv("HUB_ADMISSION_RECOVERY_MODE"); mode != "" {
		db, err := sql.Open("sqlite", filepath.Join(os.Getenv("HUB_ADMISSION_DIR"), "business.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		journal := "WAL"
		if mode == "rollback" {
			journal = "DELETE"
		}
		if _, err = db.Exec("PRAGMA journal_mode=" + journal + "; PRAGMA synchronous=FULL; PRAGMA wal_autocheckpoint=0; PRAGMA cache_size=1; PRAGMA cache_spill=ON"); err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("UPDATE metadata SET revision=2,operation_seq=3"); err != nil {
			t.Fatal(err)
		}
		if mode == "rollback" {
			raw, err := json.Marshal(domain.App{ID: "a", Platform: "fixture", ProviderAppID: "pa", RowRevision: 2, Name: "pending"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec("UPDATE apps SET record=? WHERE id='a'", string(raw)); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"apps", "shops", "brands"} {
				var count int
				if err = tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
					t.Fatal(err)
				}
			}
		} else if strings.HasPrefix(mode, "wal_salt") {
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if mode == "wal_salt_after" {
				if _, err = db.Exec("UPDATE metadata SET revision=3,operation_seq=4"); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			raw, err := json.Marshal(domain.App{ID: "b", Platform: "fixture", ProviderAppID: "pb", RowRevision: 2, Name: strings.Repeat("x", 10000)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec("INSERT INTO apps VALUES('b',?)", string(raw)); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		os.Exit(0)
	}
	flag.CommandLine = flag.NewFlagSet("hubd-admission", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = []string{"hubd-admission", "--fixture", "--state-dir", os.Getenv("HUB_ADMISSION_DIR")}
	if run() != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestDaemonMissingAuditPersistsGapBeforeIPCAndAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	if _, err := protector.NewFixture(filepath.Join(dir, "fixture.key")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "business.sqlite")
	r, err := sqlite.New(path)
	if err != nil {
		t.Fatal(err)
	}
	s := application.EmptyState()
	s.Revision = 1
	s.NextOperation = 23
	s.RecoveryClosed = true
	if err = r.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	r.Close()
	for restart := 0; restart < 2; restart++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDaemonAdmissionChild$")
		cmd.Env = append(os.Environ(), "HUB_ADMISSION_CHILD=1", "HUB_ADMISSION_DIR="+dir)
		if err = cmd.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "hub.sock"))
		}}
		client := &http.Client{Transport: transport, Timeout: time.Second}
		var health map[string]any
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			response, requestErr := client.Post("http://localhost/v1/operations", "application/json", bytes.NewBufferString(`{"version":"contract/v1","operation":"health","payload":{}}`))
			if requestErr == nil {
				requestErr = json.NewDecoder(response.Body).Decode(&health)
				response.Body.Close()
				if requestErr == nil {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		transport.CloseIdleConnections()
		if health == nil {
			cmd.Process.Kill()
			cmd.Wait()
			cancel()
			t.Fatal("missing-audit startup did not provide validated IPC")
		}
		if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
			cmd.Process.Kill()
			cmd.Wait()
			cancel()
			t.Fatal(err)
		}
		err = cmd.Wait()
		cancel()
		if err != nil {
			t.Fatal("missing-audit startup/shutdown failed", err)
		}
		if health["audit_degraded"] != true || health["revision"] != float64(1) {
			t.Fatalf("missing audit gap hidden or revision changed: %+v", health)
		}
		r, err = sqlite.New(path)
		if err != nil {
			t.Fatal(err)
		}
		loaded, loadErr := r.Load(context.Background())
		r.Close()
		if loadErr != nil || !loaded.AuditGap || !loaded.RecoveryClosed || loaded.Revision != 1 || loaded.NextOperation != 23 {
			t.Fatalf("gap/recovery state not durable: %+v %v", loaded, loadErr)
		}
	}
}

func TestAdmissionMissingAuditGapWriteFailureRejectsWithoutCreatingHistory(t *testing.T) {
	dir := t.TempDir()
	p, err := protector.NewFixture(filepath.Join(dir, "fixture.key"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "business.sqlite")
	r, err := sqlite.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// Inject a real SQLite write failure after opening a valid repository.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TRIGGER fail_gap BEFORE UPDATE ON metadata BEGIN SELECT RAISE(ABORT,'fixture write failure'); END")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(dir, "audit.sqlite")
	if err = validateAdmission(r, p, auditPath, true); err == nil {
		t.Fatal("gap persistence failure admitted")
	}
	if _, err = os.Lstat(auditPath); !os.IsNotExist(err) {
		t.Fatal("history created before durable gap")
	}
	state, err := r.Load(context.Background())
	if err != nil || state.AuditGap || state.Revision != 0 {
		t.Fatal("failed gap persistence changed business state", err)
	}
}

func TestDaemonRejectsDamagedExistingStorageBeforeIPC(t *testing.T) {
	for _, kind := range []string{"business", "audit"} {
		for _, damage := range []string{"empty", "truncated", "missing_table"} {
			t.Run(kind+"/"+damage, func(t *testing.T) {
				dir := t.TempDir()
				if _, err := protector.NewFixture(filepath.Join(dir, "fixture.key")); err != nil {
					t.Fatal(err)
				}
				r, err := sqlite.New(filepath.Join(dir, "business.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				r.Close()
				a, err := sqlite.NewAudit(filepath.Join(dir, "audit.sqlite"))
				if err != nil {
					t.Fatal(err)
				}
				a.Close()
				path := filepath.Join(dir, kind+".sqlite")
				if damage == "missing_table" {
					db, err := sql.Open("sqlite", path)
					if err != nil {
						t.Fatal(err)
					}
					table := "apps"
					if kind == "audit" {
						table = "events"
					}
					_, err = db.Exec("DROP TABLE " + table)
					db.Close()
					if err != nil {
						t.Fatal(err)
					}
				} else {
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
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDaemonAdmissionChild$")
				cmd.Env = append(os.Environ(), "HUB_ADMISSION_CHILD=1", "HUB_ADMISSION_DIR="+dir)
				output, err := cmd.CombinedOutput()
				if ctx.Err() != nil || err == nil {
					t.Errorf("damaged storage did not fail startup promptly: %v, %s", err, output)
				}
				if _, err := os.Lstat(filepath.Join(dir, "hub.sock")); !os.IsNotExist(err) {
					t.Error("IPC created before database admission")
				}
				after, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Error("damaged database repaired during failed startup")
				}
			})
		}
	}
}

func TestDaemonRejectsInvalidLegacyStateBeforeMigration(t *testing.T) {
	for _, damage := range []string{"empty_record", "identity_mismatch", "wrong_cipher"} {
		t.Run(damage, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := protector.NewFixture(filepath.Join(dir, "fixture.key")); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "business.sqlite")
			db, err := sql.Open("sqlite", path)
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
			app := domain.App{ID: "a", Platform: "fixture", ProviderAppID: "pa", RowRevision: 7}
			if damage == "identity_mismatch" {
				app.ID = "other"
			}
			raw, err := json.Marshal(app)
			if err != nil {
				t.Fatal(err)
			}
			if damage == "empty_record" {
				raw = []byte(`{}`)
			}
			if _, err = db.Exec("INSERT INTO apps VALUES('a',?)", string(raw)); err != nil {
				t.Fatal(err)
			}
			if damage == "wrong_cipher" {
				wrong, err := protector.NewFixture(filepath.Join(dir, "other.key"))
				if err != nil {
					t.Fatal(err)
				}
				cipher, err := wrong.Seal("current-credential", "app|a|", "client_secret", "fixture-value", 1)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(domain.Credential{Scope: "app", AppID: "a", Type: "client_secret", Ciphertext: cipher, Generation: 1, RowRevision: 7})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec("INSERT INTO credentials VALUES('app|a||client_secret',?)", string(raw)); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			if err = os.Chmod(path, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertDaemonRejected(t, dir)
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("invalid legacy state migrated before rejection")
			}
			db, err = sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var version int
			var schema string
			if err = db.QueryRow("SELECT version FROM metadata").Scan(&version); err != nil || version != 1 {
				t.Fatal("rejected v1 version changed", err)
			}
			if err = db.QueryRow("SELECT sql FROM sqlite_master WHERE name='metadata'").Scan(&schema); err != nil || strings.Contains(schema, "recovery_closed") {
				t.Fatal("rejected v1 schema changed", err)
			}
		})
	}
}

func assertDaemonRejected(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDaemonAdmissionChild$")
	cmd.Env = append(os.Environ(), "HUB_ADMISSION_CHILD=1", "HUB_ADMISSION_DIR="+dir)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err == nil {
		t.Errorf("unsafe storage did not fail startup: %v, %s", err, output)
	}
	if _, err = os.Lstat(filepath.Join(dir, "hub.sock")); !os.IsNotExist(err) {
		t.Error("IPC created for rejected storage")
	}
}

func admissionFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := protector.NewFixture(filepath.Join(dir, "fixture.key")); err != nil {
		t.Fatal(err)
	}
	r, err := sqlite.New(filepath.Join(dir, "business.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	s := application.EmptyState()
	s.Revision = 1
	s.NextOperation = 1
	s.Apps["a"] = domain.App{ID: "a", Platform: "fixture", ProviderAppID: "pa", RowRevision: 1}
	if err = r.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	r.Close()
	a, err := sqlite.NewAudit(filepath.Join(dir, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	return dir
}

func TestDaemonRejectsUnsafeRecoverySidecarsBeforeIPC(t *testing.T) {
	for _, kind := range []string{"business", "audit"} {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			t.Run(kind+suffix, func(t *testing.T) {
				dir := admissionFixture(t)
				path := filepath.Join(dir, kind+".sqlite")
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path+suffix, []byte("fixture-sidecar"), 0644); err != nil {
					t.Fatal(err)
				}
				assertDaemonRejected(t, dir)
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("unsafe sidecar changed database", err)
				}
				info, err := os.Stat(path + suffix)
				if err != nil || info.Mode().Perm() != 0644 {
					t.Fatal("unsafe sidecar silently repaired", err)
				}
			})
		}
	}
}

func TestDaemonRejectsIncompleteAuditHistoryBeforeIPC(t *testing.T) {
	dir := admissionFixture(t)
	path := filepath.Join(dir, "audit.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO events(record) VALUES('{}')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertDaemonRejected(t, dir)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("incomplete audit history changed", err)
	}
}

func TestDaemonRejectsCorruptedRecoveryEvidenceBeforeIPC(t *testing.T) {
	for _, damage := range []string{"wal_header", "wal_committed_page", "wal_partial_commit", "wal_salt_first", "wal_salt_after", "rollback_header", "rollback_checksum", "rollback_later_record"} {
		t.Run(damage, func(t *testing.T) {
			dir := admissionFixture(t)
			mode := "wal"
			suffix := "-wal"
			if strings.HasPrefix(damage, "rollback") {
				mode = "rollback"
				suffix = "-journal"
			}
			if strings.HasPrefix(damage, "wal_salt") {
				mode = damage
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDaemonAdmissionChild$")
			cmd.Env = append(os.Environ(), "HUB_ADMISSION_CHILD=1", "HUB_ADMISSION_DIR="+dir, "HUB_ADMISSION_RECOVERY_MODE="+mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("recovery setup failed: %v %s", err, output)
			}
			path := filepath.Join(dir, "business.sqlite")
			raw, err := os.ReadFile(path + suffix)
			if err != nil || len(raw) < 512 {
				t.Fatal("recovery evidence missing", err)
			}
			if strings.HasPrefix(damage, "rollback") && bytes.Equal(raw[:8], make([]byte, 8)) {
				t.Fatal("rollback fixture is not hot")
			}
			switch damage {
			case "wal_salt_first", "wal_salt_after":
				page := int(binary.BigEndian.Uint32(raw[8:12]))
				frames := 1
				if damage == "wal_salt_after" {
					frames = 2
				}
				if len(raw) != 32+frames*(page+24) {
					t.Fatal("salt fixture needs only committed metadata frames")
				}
				off := 32 + (frames-1)*(page+24)
				if binary.BigEndian.Uint32(raw[off+4:off+8]) == 0 {
					t.Fatal("salt fixture needs a committed final frame")
				}
				salt := binary.BigEndian.Uint32(raw[off+8 : off+12])
				bit := salt & (^salt + 1)
				if bit == 0 || bit >= 1<<31 {
					t.Skip("generated salt has no lower set bit")
				}
				binary.BigEndian.PutUint32(raw[off+8:off+12], salt&^bit)
				raw[off+15] ^= 1
			case "wal_committed_page":
				raw[56] ^= 0xff
			case "wal_partial_commit":
				page := int(binary.BigEndian.Uint32(raw[8:12]))
				off := len(raw) - (page + 24)
				if binary.BigEndian.Uint32(raw[off+4:off+8]) == 0 {
					t.Fatal("fixture needs final commit")
				}
				raw = raw[:off+24+page/2]
			case "rollback_checksum":
				sector := int(binary.BigEndian.Uint32(raw[20:24]))
				page := int(binary.BigEndian.Uint32(raw[24:28]))
				raw[sector+4+page] ^= 0xff
			case "rollback_later_record":
				sector := int(binary.BigEndian.Uint32(raw[20:24]))
				page := int(binary.BigEndian.Uint32(raw[24:28]))
				count := int(binary.BigEndian.Uint32(raw[8:12]))
				next := (sector + count*(page+8) + sector - 1) / sector * sector
				if next+sector+page+8 > len(raw) || bytes.Equal(raw[next:next+8], make([]byte, 8)) {
					t.Fatal("fixture needs second hot journal chunk")
				}
				raw = raw[:next+sector+4+page/2]
			default:
				raw[0] ^= 0xff
			}
			if err = os.WriteFile(path+suffix, raw, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			assertDaemonRejected(t, dir)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("corrupt recovery evidence changed database", err)
			}
			after, err = os.ReadFile(path + suffix)
			if err != nil || !bytes.Equal(raw, after) {
				t.Fatal("corrupt recovery evidence discarded", err)
			}
		})
	}
}
