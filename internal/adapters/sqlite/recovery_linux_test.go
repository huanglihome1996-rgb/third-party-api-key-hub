//go:build linux

package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/third-party-api-key-hub/internal/application"
	"github.com/third-party-api-key-hub/internal/domain"
)

// Exit without closing the connection/transaction to leave genuine SQLite
// recovery files, rather than fabricating journals or checkpointed WAL copies.
func TestSQLiteCrashChild(t *testing.T) {
	mode := os.Getenv("HUB_SQLITE_CRASH_MODE")
	if mode == "" {
		return
	}
	db, err := sql.Open("sqlite", os.Getenv("HUB_SQLITE_CRASH_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	journal := "DELETE"
	if strings.HasPrefix(mode, "wal") {
		journal = "WAL"
	}
	if _, err = db.Exec("PRAGMA journal_mode=" + journal + "; PRAGMA synchronous=FULL; PRAGMA wal_autocheckpoint=0; PRAGMA cache_size=1; PRAGMA cache_spill=ON"); err != nil {
		t.Fatal(err)
	}
	if mode == "wal_rollback_tail" {
		if _, err = db.Exec("UPDATE metadata SET revision=2"); err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 100; i++ {
			id := fmt.Sprintf("pending%d", i)
			raw, err := json.Marshal(domain.App{ID: id, Platform: "fixture", ProviderAppID: id, RowRevision: 3, Name: strings.Repeat("x", 10000)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec("INSERT INTO apps VALUES(?,?)", id, string(raw)); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("UPDATE metadata SET revision=8,operation_seq=12"); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	audit := os.Getenv("HUB_SQLITE_CRASH_AUDIT") == "1"
	if !audit && mode != "wal_reuse_pending" {
		if _, err = tx.Exec("UPDATE metadata SET revision=8,operation_seq=12"); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "rollback_metadata" {
		raw, err := json.Marshal(domain.App{ID: "a", Platform: "fixture", ProviderAppID: "pa", RowRevision: 8, Name: "pending"})
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
		os.Exit(0)
	}
	count := 1
	if strings.HasPrefix(mode, "wal_salt") {
		count = 0
	}
	if mode == "rollback" || mode == "wal_reuse" || mode == "wal_reuse_pending" {
		count = 100
	}
	for i := 0; i < count; i++ {
		if audit {
			raw, err := json.Marshal(application.AuditEvent{Time: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Operation: "crash_committed", Target: strings.Repeat("x", 10000), Result: "success", OperationID: "op-crash"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec("INSERT INTO events(record) VALUES(?)", string(raw)); err != nil {
				t.Fatal(err)
			}
		} else {
			id := fmt.Sprintf("b%d", i)
			revision := int64(8)
			if mode == "wal_reuse_pending" {
				revision = 1
			}
			raw, err := json.Marshal(domain.App{ID: id, Platform: "fixture", ProviderAppID: id, RowRevision: revision, Name: strings.Repeat("x", 10000)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec("INSERT INTO apps VALUES(?,?)", id, string(raw)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if strings.HasPrefix(mode, "wal") {
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "wal_reuse" {
		if _, err = db.Exec("PRAGMA wal_checkpoint(RESTART)"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("UPDATE metadata SET revision=9,operation_seq=13"); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "wal_reuse_pending" {
		if _, err = db.Exec("PRAGMA wal_checkpoint(RESTART)"); err != nil {
			t.Fatal(err)
		}
		tx, err = db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("UPDATE metadata SET revision=8,operation_seq=12"); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"apps", "shops", "brands"} {
			var count int
			if err = tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
				t.Fatal(err)
			}
		}
	}
	if mode == "wal_salt_after" {
		if _, err = db.Exec("UPDATE metadata SET revision=9,operation_seq=13"); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "wal_tail" || mode == "wal_later_commit" {
		tx, err = db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("UPDATE metadata SET revision=9,operation_seq=13"); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			raw, err := json.Marshal(domain.App{ID: fmt.Sprintf("pending%d", i), Platform: "fixture", ProviderAppID: fmt.Sprintf("pending%d", i), RowRevision: 9, Name: strings.Repeat("x", 10000)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec("INSERT INTO apps VALUES(?,?)", fmt.Sprintf("pending%d", i), string(raw)); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "wal_later_commit" {
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
	}
	os.Exit(0)
}

func TestWALRestartPendingPrefixRecoversCheckpointedState(t *testing.T) {
	testWALPendingSuffixRecovery(t, "wal_reuse_pending", 1, 11, 100)
}

func TestWALRolledBackSuffixRecoversLatestCommit(t *testing.T) {
	testWALPendingSuffixRecovery(t, "wal_rollback_tail", 8, 12, 0)
}

func testWALPendingSuffixRecovery(t *testing.T, mode string, wantRevision, wantNext, wantCount int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "business.sqlite")
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	s := application.EmptyState()
	s.Revision = 1
	s.NextOperation = 11
	s.RecoveryClosed = true
	s.AuditGap = true
	if err = r.Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSQLiteCrashChild$")
	cmd.Env = append(os.Environ(), "HUB_SQLITE_CRASH_MODE="+mode, "HUB_SQLITE_CRASH_PATH="+path, "HUB_SQLITE_CRASH_AUDIT=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crash setup failed: %v %s", err, output)
	}
	raw, err := os.ReadFile(path + "-wal")
	if err != nil || len(raw) < 32 {
		t.Fatal("reused WAL missing", err)
	}
	stride := int(binary.BigEndian.Uint32(raw[8:12])) + 24
	if mode == "wal_reuse_pending" {
		if len(raw) < 32+2*stride || !bytes.Equal(raw[40:48], raw[16:24]) || binary.BigEndian.Uint32(raw[36:40]) != 0 || bytes.Equal(raw[32+stride+8:32+stride+16], raw[16:24]) {
			t.Fatal("fixture needs a current pending first frame followed by an old generation")
		}
	} else {
		if len(raw) < 32+3*stride || binary.BigEndian.Uint32(raw[36+stride:40+stride]) == 0 {
			t.Fatal("fixture needs two current commits followed by a rolled-back suffix")
		}
		for off := 32 + 2*stride; off+stride <= len(raw); off += stride {
			if binary.BigEndian.Uint32(raw[off+4:off+8]) != 0 || !bytes.Equal(raw[off+8:off+16], raw[16:24]) {
				t.Fatal("rolled-back suffix needs only current-generation pending frames")
			}
		}
	}
	// Recover a separate copy through SQLite itself to establish the expected
	// checkpointed state without changing the repository admission fixture.
	oraclePath := filepath.Join(t.TempDir(), "oracle.sqlite")
	database, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{oraclePath: database, oraclePath + "-wal": raw} {
		if err = os.WriteFile(name, contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	oracle, err := sql.Open("sqlite", oraclePath)
	if err != nil {
		t.Fatal(err)
	}
	var revision, next, count int
	err = oracle.QueryRow("SELECT revision,operation_seq,(SELECT COUNT(*) FROM apps) FROM metadata").Scan(&revision, &next, &count)
	oracle.Close()
	if err != nil || revision != wantRevision || next != wantNext || count != wantCount {
		t.Fatalf("SQLite recovery oracle: revision=%d next=%d apps=%d err=%v", revision, next, count, err)
	}
	r, err = New(path)
	if err != nil {
		t.Fatal("legitimate pending suffix rejected", err)
	}
	defer r.Close()
	actual, err := r.Load(context.Background())
	if err != nil || actual.Revision != int64(wantRevision) || actual.NextOperation != uint64(wantNext) || len(actual.Apps) != wantCount || !actual.RecoveryClosed || !actual.AuditGap {
		t.Fatalf("checkpointed state lost during pending prefix recovery: %+v %v", actual, err)
	}
}

func TestWALSaltDamageCannotMasqueradeAsStaleGeneration(t *testing.T) {
	for _, mode := range []string{"wal_salt_first", "wal_salt_after"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "business.sqlite")
			r, err := New(path)
			if err != nil {
				t.Fatal(err)
			}
			s := application.EmptyState()
			s.Revision = 1
			s.NextOperation = 11
			s.RecoveryClosed = true
			s.AuditGap = true
			if err = r.Save(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			r.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSQLiteCrashChild$")
			cmd.Env = append(os.Environ(), "HUB_SQLITE_CRASH_MODE="+mode, "HUB_SQLITE_CRASH_PATH="+path, "HUB_SQLITE_CRASH_AUDIT=0")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("crash setup: %v %s", err, output)
			}
			raw, err := os.ReadFile(path + "-wal")
			if err != nil {
				t.Fatal(err)
			}
			page := int(binary.BigEndian.Uint32(raw[8:12]))
			stride := page + 24
			frames := 1
			if mode == "wal_salt_after" {
				frames = 2
			}
			if len(raw) != 32+frames*stride {
				t.Fatalf("expected exactly %d committed metadata frame(s), bytes=%d", frames, len(raw))
			}
			off := 32 + (frames-1)*stride
			if binary.BigEndian.Uint32(raw[off+4:off+8]) == 0 {
				t.Fatal("fixture needs a committed final frame")
			}
			salt := binary.BigEndian.Uint32(raw[off+8 : off+12])
			bit := salt & (^salt + 1)
			if bit == 0 || bit >= 1<<31 {
				t.Skip("generated salt has no lower set bit")
			}
			checksums := append([]byte(nil), raw[off+16:off+24]...)
			binary.BigEndian.PutUint32(raw[off+8:off+12], salt&^bit)
			raw[off+15] ^= 1
			if !bytes.Equal(checksums, raw[off+16:off+24]) {
				t.Fatal("salt damage must leave stored checksums unchanged")
			}
			if err = os.WriteFile(path+"-wal", raw, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if r, err = New(path); err == nil {
				actual, _ := r.Load(context.Background())
				r.Close()
				t.Errorf("altered active salts accepted as stale tail: recovered revision=%d", actual.Revision)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Error("database changed while rejecting corrupted salts")
			}
			after, err = os.ReadFile(path + "-wal")
			if err != nil || !bytes.Equal(raw, after) {
				t.Error("corrupted WAL evidence discarded")
			}
		})
	}
}

func TestDamagedRecoveryFilesAndUncommittedWALTail(t *testing.T) {
	for _, damage := range []string{"wal_header", "wal_committed_page", "wal_pending_before_later_commit", "wal_partial_commit", "rollback_header", "rollback_checksum", "rollback_later_record", "wal_uncommitted_tail", "wal_reuse"} {
		t.Run(damage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "business.sqlite")
			r, err := New(path)
			if err != nil {
				t.Fatal(err)
			}
			s := application.EmptyState()
			s.Revision = 1
			s.NextOperation = 11
			s.RecoveryClosed = true
			s.AuditGap = true
			s.Apps["a"] = domain.App{ID: "a", Platform: "fixture", ProviderAppID: "pa", RowRevision: 1}
			if err = r.Save(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			r.Close()
			mode := "wal"
			suffix := "-wal"
			if strings.HasPrefix(damage, "rollback") {
				mode = "rollback_metadata"
				suffix = "-journal"
			}
			if damage == "wal_uncommitted_tail" {
				mode = "wal_tail"
			}
			if damage == "wal_pending_before_later_commit" {
				mode = "wal_later_commit"
			}
			if damage == "wal_reuse" {
				mode = "wal_reuse"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSQLiteCrashChild$")
			cmd.Env = append(os.Environ(), "HUB_SQLITE_CRASH_MODE="+mode, "HUB_SQLITE_CRASH_PATH="+path, "HUB_SQLITE_CRASH_AUDIT=0")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("crash setup failed: %v %s", err, output)
			}
			raw, err := os.ReadFile(path + suffix)
			if err != nil || len(raw) < 512 {
				t.Fatalf("recovery file missing: %v", err)
			}
			if strings.HasPrefix(damage, "rollback") && bytes.Equal(raw[:8], make([]byte, 8)) {
				t.Fatal("test requires a genuinely hot journal")
			}
			switch damage {
			case "wal_header":
				raw[0] ^= 0xff
			case "rollback_header":
				raw[0] ^= 0xff
			case "wal_committed_page":
				raw[56] ^= 0xff
			case "wal_pending_before_later_commit":
				stride := int(binary.BigEndian.Uint32(raw[8:12])) + 24
				off := 32
				for off+stride <= len(raw) && binary.BigEndian.Uint32(raw[off+4:off+8]) == 0 {
					off += stride
				}
				off += stride
				if off+stride > len(raw) || binary.BigEndian.Uint32(raw[off+4:off+8]) != 0 || binary.BigEndian.Uint32(raw[len(raw)-stride+4:len(raw)-stride+8]) == 0 {
					t.Fatal("fixture needs a committed prefix and a pending frame before a later commit")
				}
				raw[off+24] ^= 0xff
			case "wal_partial_commit":
				page := int(binary.BigEndian.Uint32(raw[8:12]))
				stride := page + 24
				off := len(raw) - stride
				if binary.BigEndian.Uint32(raw[off+4:off+8]) == 0 {
					t.Fatal("test requires last committed frame")
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
					t.Fatal("fixture requires second hot journal chunk")
				}
				raw = raw[:next+sector+4+page/2]
			case "wal_uncommitted_tail":
				page := int(binary.BigEndian.Uint32(raw[8:12]))
				off := len(raw) - (page + 24)
				if binary.BigEndian.Uint32(raw[off+4:off+8]) != 0 {
					t.Fatal("fixture requires pending final frame")
				}
				raw = raw[:off+24+page/2]
			}
			if err = os.WriteFile(path+suffix, raw, 0600); err != nil {
				t.Fatal(err)
			}
			r, err = New(path)
			if damage == "wal_uncommitted_tail" || damage == "wal_reuse" {
				if err != nil {
					t.Fatal("recoverable uncommitted tail rejected", err)
				}
				defer r.Close()
				actual, err := r.Load(context.Background())
				revision := int64(8)
				next := uint64(12)
				count := 2
				if damage == "wal_reuse" {
					revision = 9
					next = 13
					count = 101
				}
				if err != nil || actual.Revision != revision || actual.NextOperation != next || len(actual.Apps) != count || !actual.RecoveryClosed || !actual.AuditGap {
					t.Fatalf("committed state lost during tail recovery: %+v %v", actual, err)
				}
			} else if err == nil {
				actual, _ := r.Load(context.Background())
				r.Close()
				t.Errorf("corrupted recovery file admitted: revision=%d apps=%d", actual.Revision, len(actual.Apps))
			}
		})
	}
}

func TestAdmissionRecoversCommittedWALAndHotRollbackJournal(t *testing.T) {
	for _, audit := range []bool{false, true} {
		for _, mode := range []string{"wal", "rollback"} {
			t.Run(fmt.Sprintf("audit=%v/%s", audit, mode), func(t *testing.T) {
				ctx := context.Background()
				path := filepath.Join(t.TempDir(), "database.sqlite")
				if audit {
					a, err := NewAudit(path)
					if err != nil {
						t.Fatal(err)
					}
					if err = a.Record(ctx, application.AuditEvent{Time: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), Operation: "before_crash", Target: "unknown", Result: "success", OperationID: "op-before"}); err != nil {
						t.Fatal(err)
					}
					a.Close()
				} else {
					r, err := New(path)
					if err != nil {
						t.Fatal(err)
					}
					s := application.EmptyState()
					s.Revision = 1
					s.NextOperation = 11
					s.RecoveryClosed = true
					s.AuditGap = true
					s.Apps["a"] = domain.App{ID: "a", Platform: "fixture", ProviderAppID: "pa", RowRevision: 1}
					if err = r.Save(ctx, s); err != nil {
						t.Fatal(err)
					}
					r.Close()
				}
				childCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestSQLiteCrashChild$")
				auditFlag := "0"
				if audit {
					auditFlag = "1"
				}
				cmd.Env = append(os.Environ(), "HUB_SQLITE_CRASH_MODE="+mode, "HUB_SQLITE_CRASH_PATH="+path, "HUB_SQLITE_CRASH_AUDIT="+auditFlag)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("crash setup: %v, %s", err, output)
				}
				suffix := "-journal"
				if mode == "wal" {
					suffix = "-wal"
				}
				journal, err := os.ReadFile(path + suffix)
				if err != nil || len(journal) < 512 {
					t.Fatalf("recovery log missing: %v, bytes=%d", err, len(journal))
				}
				if mode == "rollback" && string(journal[:8]) == strings.Repeat("\x00", 8) {
					t.Fatal("rollback journal is not hot")
				}
				if audit {
					a, err := NewAudit(path)
					if err != nil {
						t.Fatal(err)
					}
					defer a.Close()
					events, err := a.List(ctx)
					expected := 1
					if mode == "wal" {
						expected = 2
					}
					if err != nil || len(events) != expected || events[0].Operation != "before_crash" {
						t.Fatalf("audit recovery lost committed history: %+v %v", events, err)
					}
				} else {
					// Immutable snapshot reads would miss these committed WAL frames.
					if mode == "wal" {
						s, err := Inspect(ctx, path, false)
						if err != nil || s.Revision != 1 {
							t.Fatalf("WAL fixture unexpectedly checkpointed: %+v %v", s, err)
						}
					}
					r, err := New(path)
					if err != nil {
						t.Fatal(err)
					}
					defer r.Close()
					s, err := r.Load(ctx)
					revision := int64(1)
					next := uint64(11)
					count := 1
					if mode == "wal" {
						revision = 8
						next = 12
						count = 2
					}
					if err != nil || s.Revision != revision || s.NextOperation != next || len(s.Apps) != count || !s.RecoveryClosed || !s.AuditGap {
						t.Fatalf("business recovery lost committed state: %+v %v", s, err)
					}
				}
			})
		}
	}
}
