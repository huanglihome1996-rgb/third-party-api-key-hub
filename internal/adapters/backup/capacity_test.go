//go:build linux

package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/third-party-api-key-hub/internal/adapters/sqlite"
	"github.com/third-party-api-key-hub/internal/application"
)

// TestHistoricalCapacity is deliberately opt-in: it writes only synthetic,
// non-secret audit records and measures complete validation without caching.
// HUB_CAPACITY_EVENTS and HUB_CAPACITY_PACKAGES make the measured envelope
// explicit; ordinary tests must not claim production capacity from this fixture.
func TestHistoricalCapacity(t *testing.T) {
	if os.Getenv("HUB_CAPACITY_EVENTS") == "" {
		t.Skip("opt-in historical capacity measurement")
	}
	report := map[string]any{"mode": "fixture", "production_capacity_acceptance": "pending", "go_version": runtime.Version(), "architecture": runtime.GOARCH, "gomaxprocs": runtime.GOMAXPROCS(0), "full_validation": false, "secret_cache": false, "automatic_maintenance_target_ms": 120000}
	errors := map[string]string{}
	report["errors"] = errors
	phase := "configuration"
	// Register before fixture creation or seeding: every attempted opt-in run
	// emits a report, including setup and partial-measurement failures.
	defer func() {
		report["phase"] = phase
		report["measured_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		report["status"] = "PASS"
		if t.Failed() {
			report["status"] = "FAIL"
			if len(errors) == 0 {
				errors[phase] = "setup_failed"
			}
		}
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		t.Log(string(raw))
		if output := os.Getenv("HUB_CAPACITY_REPORT"); output != "" {
			if err := os.WriteFile(output, append(raw, '\n'), 0600); err != nil {
				t.Error(err)
			}
		}
	}()
	events, err := strconv.Atoi(os.Getenv("HUB_CAPACITY_EVENTS"))
	if err != nil || events < 1 || events > 1000000 {
		t.Fatal("invalid capacity event count")
	}
	packages, err := strconv.Atoi(os.Getenv("HUB_CAPACITY_PACKAGES"))
	if err != nil || packages < 1 || packages > 366 {
		t.Fatal("invalid capacity package count")
	}
	report["audit_events"] = events + 2
	report["packages"] = packages
	if raw, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if affinity, found := strings.CutPrefix(line, "Cpus_allowed_list:"); found {
				report["cpu_affinity"] = strings.TrimSpace(affinity)
			}
		}
	}
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if total, found := strings.CutPrefix(line, "MemTotal:"); found {
				report["host_memory_total"] = strings.TrimSpace(total)
			}
		}
	}
	if raw, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		report["kernel_release"] = strings.TrimSpace(string(raw))
	}
	for name, path := range map[string]string{"cgroup_cpu_max": "/sys/fs/cgroup/cpu.max", "cgroup_memory_max": "/sys/fs/cgroup/memory.max"} {
		if raw, err := os.ReadFile(path); err == nil {
			report[name] = strings.TrimSpace(string(raw))
		} else {
			report[name] = "unavailable"
		}
	}
	phase = "fixture_creation"
	m, state, root := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	phase = "history_seeding"
	db, err := sql.Open("sqlite", filepath.Join(root, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO events(record) VALUES(?)")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	for i := 0; i < events; i++ {
		// Half the history is retained; half is older than the full-year cutoff.
		at := now.Add(-time.Duration(i%366) * 24 * time.Hour)
		if i%2 == 0 {
			at = at.Add(-367 * 24 * time.Hour)
		}
		raw, err := json.Marshal(application.AuditEvent{Time: at, UID: 10001, ActorName: "fixture-capacity", Operation: "app.list", OperationID: fmt.Sprintf("op-capacity-%d", i), Result: "success", Target: "app_id:a"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = stmt.ExecContext(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()
	phase = "measurements"
	measure := func(name string, run func() error) float64 {
		t.Helper()
		started := time.Now()
		if err := run(); err != nil {
			errors[name] = "measurement_failed"
			t.Errorf("%s failed: %v", name, err)
		}
		return float64(time.Since(started).Microseconds()) / 1000
	}
	phase = "snapshot_creation"
	report["backup_ms"] = measure("backup", func() error { _, err := m.Backup(ctx, state.Revision, false, func() bool { return true }); return err })
	entries, err := os.ReadDir(m.Root)
	if err != nil || len(entries) != 1 {
		t.Fatal("missing initial package", err)
	}
	source := filepath.Join(m.Root, entries[0].Name())
	var packageBytes int64
	for _, name := range []string{"business.sqlite", "audit.sqlite", "manifest.json"} {
		info, err := os.Stat(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		packageBytes += info.Size()
	}
	report["package_bytes"] = packageBytes
	phase = "package_history_copy"
	for i := 1; i < packages; i++ {
		dest := filepath.Join(m.Root, fmt.Sprintf("backup-capacity-%03d", i))
		if err := os.Mkdir(dest, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"business.sqlite", "audit.sqlite", "manifest.json"} {
			raw, err := os.ReadFile(filepath.Join(source, name))
			if err != nil {
				t.Fatal(err)
			}
			if name == "manifest.json" {
				var manifest Manifest
				if err := json.Unmarshal(raw, &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.CreatedAt = now.Add(-time.Duration(i) * 24 * time.Hour).Format(time.RFC3339Nano)
				raw, err = json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dest, name), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	phase = "measurements"
	report["schedule_ms"] = measure("schedule", func() error {
		_, invalid, due, err := m.Schedule(ctx, time.Now().UTC())
		if err == nil && (invalid != 0 || due) {
			return fmt.Errorf("schedule correctness failed")
		}
		report["full_validation"] = err == nil
		return err
	})
	report["inspect_audit_ms"] = measure("inspect_audit", func() error { _, err := sqlite.Inspect(ctx, filepath.Join(root, "audit.sqlite"), true); return err })
	report["audit_list_ms"] = measure("audit_list", func() error {
		all, err := m.Audit.List(ctx)
		if err == nil && len(all) != events+2 {
			return fmt.Errorf("audit count mismatch")
		}
		return err
	})
	report["retention_ms"] = measure("retention", func() error {
		removed, _, err := m.Audit.Retain(ctx, now)
		if err == nil && removed != int64((events+1)/2+2) {
			return fmt.Errorf("retention count mismatch: %d", removed)
		}
		return err
	})
	// Keep every measured outcome in the report before failing the budget gate.
	// This is a conservative cycle including a fresh snapshot even when not due.
	cycle := report["schedule_ms"].(float64) + report["backup_ms"].(float64) + report["retention_ms"].(float64)
	report["complete_maintenance_cycle_ms"] = cycle
	if cycle > 120000 {
		errors["complete_maintenance_cycle"] = "budget_exceeded"
		t.Errorf("complete historical maintenance exceeds 120000ms: %.3fms", cycle)
	}
	phase = "completed"
}
