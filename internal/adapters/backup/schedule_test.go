//go:build linux

package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/application"
)

func TestScheduleCancellationCannotReturnSuccessfulEmptyHistory(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "empty"}[exists], func(t *testing.T) {
			m, _, _ := fixture(t)
			if exists {
				if err := os.Mkdir(m.Root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			next, invalid, due, err := m.Schedule(ctx, time.Now().UTC())
			if !errors.Is(err, context.Canceled) || !next.IsZero() || invalid != 0 || !due {
				t.Fatal("cancelled empty-history scan reported success or corruption", err, next, invalid, due)
			}
		})
	}
}

func TestSchedulePausesForForegroundWorkAndResumesCompleteValidation(t *testing.T) {
	m, state, _ := fixture(t)
	ctx := context.Background()
	var paths []string
	for range 6 {
		result, err := m.Backup(ctx, state.Revision, false, func() bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Join(m.Root, result.BackupID))
	}
	finishFirst := m.beginForeground()
	finishSecond := m.beginForeground()
	type observation struct {
		invalid int
		err     error
	}
	done := make(chan observation, 1)
	go func() {
		_, invalid, _, err := m.Schedule(ctx, time.Now().UTC())
		done <- observation{invalid: invalid, err: err}
	}()
	assertPaused := func() {
		t.Helper()
		select {
		case got := <-done:
			t.Fatalf("schedule completed while foreground work was active: %+v", got)
		case <-time.After(20 * time.Millisecond):
		}
	}
	assertPaused()
	finishFirst()
	assertPaused()
	// Every package is damaged while workers are paused. Resuming must inspect
	// them all rather than treating the pause as successful or skipping jobs.
	for _, path := range paths {
		if err := os.WriteFile(filepath.Join(path, "business.sqlite"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	finishSecond()
	select {
	case got := <-done:
		if got.err != nil || got.invalid != len(paths) {
			t.Fatalf("resumed scan did not inspect every package: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("schedule failed to resume after foreground work")
	}
}

func TestScheduleCancellationWhilePausedDoesNotRequireForegroundCompletion(t *testing.T) {
	m, state, _ := fixture(t)
	if _, err := m.Backup(context.Background(), state.Revision, false, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	finish := m.beginForeground()
	defer finish()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		next, invalid, _, err := m.Schedule(ctx, time.Now().UTC())
		if !next.IsZero() || invalid != 0 {
			done <- fmt.Errorf("cancelled paused scan produced schedule or corruption: %v %d", next, invalid)
			return
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("schedule completed before cancellation while foreground active", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("paused cancellation was not returned", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled schedule waited for foreground completion")
	}
}

func TestScheduleDeadlineDoesNotClassifyValidHistoryAsCorruption(t *testing.T) {
	m, state, root := fixture(t)
	ctx := context.Background()
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
	for i := 0; i < 10000; i++ {
		raw, err := json.Marshal(application.AuditEvent{Time: time.Now().UTC().Add(-time.Hour), Operation: "app.list", Target: "app_id:a", Result: "success", OperationID: fmt.Sprintf("op-valid-history-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stmt.ExecContext(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := m.Backup(ctx, state.Revision, false, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()
	next, invalid, due, err := m.Schedule(deadline, time.Now().UTC())
	if !errors.Is(err, context.DeadlineExceeded) || !next.IsZero() || invalid != 0 || !due {
		t.Fatal("deadline was reported as success or corrupt history", next, invalid, due, err)
	}
}

func TestScheduleRevalidatesAllHistoryAndIndependentKey(t *testing.T) {
	m, state, root := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	var paths []string
	for i := 0; i < 6; i++ {
		result, err := m.Backup(ctx, state.Revision, false, func() bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(m.Root, result.BackupID)
		paths = append(paths, path)
		raw, err := os.ReadFile(filepath.Join(path, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest Manifest
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.CreatedAt = now.Add(-time.Duration(i+1) * time.Hour).Format(time.RFC3339Nano)
		raw, err = json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "manifest.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want := now.Add(23 * time.Hour)
	assertSchedule := func(count int, expected time.Time) {
		t.Helper()
		next, invalid, due, err := m.Schedule(ctx, now)
		if err != nil || invalid != count || due || !next.Equal(expected) {
			t.Fatal("historical scan result changed", next, invalid, due, err)
		}
	}
	assertSchedule(0, want)
	// Alter an old package after a successful scan and update its digest. The
	// package must still pass complete audit schema/integrity/record validation.
	old := paths[len(paths)-1]
	if err := os.WriteFile(filepath.Join(old, "audit.sqlite"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(old, "manifest.json"))
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	var err error
	manifest.Files["audit.sqlite"], err = digest(filepath.Join(old, "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(m.Root, ".incomplete-not-published"), 0700); err != nil {
		t.Fatal(err)
	}
	assertSchedule(1, want)
	// The latest corrupt package cannot postpone backup using its manifest.
	if err := os.WriteFile(filepath.Join(paths[0], "business.sqlite"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	assertSchedule(2, now.Add(22*time.Hour))
	original, err := os.ReadFile(m.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "other.key")
	if _, err := protector.NewFixture(other); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.KeyFile, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	next, invalid, due, err := m.Schedule(ctx, now)
	if err != nil || invalid != len(paths) || !due || !next.Equal(now) {
		t.Fatal("cached key binding reused", next, invalid, due, err)
	}
	if err := os.WriteFile(m.KeyFile, original, 0600); err != nil {
		t.Fatal(err)
	}
	assertSchedule(2, now.Add(22*time.Hour))
}
