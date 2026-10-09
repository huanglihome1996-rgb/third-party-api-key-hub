// Package backup publishes consistent encrypted SQLite snapshots. Only the
// service's frozen maintenance boundary calls Manager.Backup.
package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/adapters/sqlite"
	"github.com/third-party-api-key-hub/internal/application"
	"github.com/third-party-api-key-hub/internal/buildinfo"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"github.com/third-party-api-key-hub/internal/domain"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const ConfigBinding = "fixture-profile-v1:uid-owner:local-only"

type Manifest struct {
	Version        int               `json:"version"`
	Schema         int               `json:"schema"`
	Mode           string            `json:"mode"`
	Config         string            `json:"config"`
	Build          string            `json:"build"`
	KeyFingerprint string            `json:"key_fingerprint"`
	KeyVersion     int               `json:"key_version"`
	Revision       int64             `json:"revision"`
	AuditGap       bool              `json:"audit_gap"`
	CreatedAt      string            `json:"created_at"`
	Files          map[string]string `json:"files"`
}
type Manager struct {
	Root, KeyFile string
	Repo          *sqlite.Repository
	Audit         *sqlite.Audit
	maintenanceMu sync.Mutex
	foreground    int
	resume        chan struct{}
}

// Foreground maintenance already holds the application's maintenance boundary.
// Stop dispatching further historical validations while it runs so background
// CPU work does not extend the interval during which requests are waiting.
// In-flight package validation may finish; foreground work never waits on it.
func (m *Manager) beginForeground() func() {
	m.maintenanceMu.Lock()
	if m.foreground == 0 {
		m.resume = make(chan struct{})
	}
	m.foreground++
	m.maintenanceMu.Unlock()
	return func() {
		m.maintenanceMu.Lock()
		defer m.maintenanceMu.Unlock()
		m.foreground--
		if m.foreground == 0 {
			close(m.resume)
			m.resume = nil
		}
	}
}

func (m *Manager) waitBackground(ctx context.Context) error {
	for {
		m.maintenanceMu.Lock()
		resume := m.resume
		m.maintenanceMu.Unlock()
		if resume == nil {
			return ctx.Err()
		}
		select {
		case <-resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Schedule derives due time from verified durable packages rather than process
// uptime. A restart cannot postpone a missed daily snapshot. Invalid packages
// are counted for fixed-enum alerts and never replace the last valid package.
func (m *Manager) Schedule(ctx context.Context, now time.Time) (time.Time, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, 0, true, err
	}
	if SafePath(m.Root) != nil {
		return time.Time{}, 0, true, errors.New("unsafe backup root")
	}
	entries, err := os.ReadDir(m.Root)
	if ctx.Err() != nil {
		return time.Time{}, 0, true, ctx.Err()
	}
	if errors.Is(err, os.ErrNotExist) {
		return now.UTC(), 0, true, nil
	}
	if err != nil {
		return time.Time{}, 0, true, err
	}
	var latest time.Time
	invalid := 0
	// Independently verify every package, with a small CPU/connection/memory
	// bound. Results retain only timestamps, never decrypted state or a cached
	// validation decision. A cancelled scan cannot produce a successful schedule.
	type result struct {
		created time.Time
		valid   bool
	}
	workers := min(4, runtime.GOMAXPROCS(0), len(entries))
	jobs := make(chan string)
	results := make(chan result, workers)
	var pending sync.WaitGroup
	for range workers {
		pending.Add(1)
		go func() {
			defer pending.Done()
			for path := range jobs {
				if m.waitBackground(ctx) != nil {
					return
				}
				manifest, _, err := Validate(ctx, path, m.KeyFile)
				if ctx.Err() != nil {
					return
				}
				at, _ := time.Parse(time.RFC3339Nano, manifest.CreatedAt)
				observed := result{created: at, valid: err == nil && !at.After(now.UTC())}
				select {
				case results <- observed:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, entry := range entries {
			// Private staging is never a published recovery point, including
			// concurrent writes and abandoned stages after a crash.
			if strings.HasPrefix(entry.Name(), ".incomplete-") {
				continue
			}
			select {
			case jobs <- filepath.Join(m.Root, entry.Name()):
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { pending.Wait(); close(results) }()
	for observed := range results {
		if !observed.valid {
			invalid++
			continue
		}
		if observed.created.After(latest) {
			latest = observed.created
		}
	}
	if ctx.Err() != nil {
		return time.Time{}, invalid, true, ctx.Err()
	}
	if latest.IsZero() {
		return now.UTC(), invalid, true, nil
	}
	next := latest.Add(24 * time.Hour)
	return next, invalid, !now.UTC().Before(next), nil
}

// SafePath rejects symlinks in all existing components, including parent paths.
func SafePath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe path")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}
func fingerprint(path string) (string, error) {
	if err := SafePath(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != 32 {
		return "", errors.New("invalid independent key")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func digest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func write(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}
func (m *Manager) Backup(ctx context.Context, revision int64, gap bool, allowed func() bool) (v1.BackupResult, error) {
	finish := m.beginForeground()
	defer finish()
	// Capture the conservative RPO reference before any snapshot work.
	created := time.Now().UTC().Format(time.RFC3339Nano)
	out := v1.BackupResult{}
	if err := SafePath(m.Root); err != nil {
		return out, err
	}
	if err := os.MkdirAll(m.Root, 0700); err != nil {
		return out, err
	}
	if err := os.Chmod(m.Root, 0700); err != nil {
		return out, err
	}
	fp, err := fingerprint(m.KeyFile)
	if err != nil {
		return out, err
	}
	stage, err := os.MkdirTemp(m.Root, ".incomplete-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(stage)
	if err = m.Repo.Snapshot(ctx, filepath.Join(stage, "business.sqlite")); err != nil {
		return out, err
	}
	if err = m.Audit.Snapshot(ctx, filepath.Join(stage, "audit.sqlite")); err != nil {
		return out, err
	}
	state, err := sqlite.Inspect(ctx, filepath.Join(stage, "business.sqlite"), false)
	if err != nil || state.Revision != revision {
		return out, errors.New("snapshot revision rejected")
	}
	if _, err = sqlite.Inspect(ctx, filepath.Join(stage, "audit.sqlite"), true); err != nil {
		return out, err
	}
	p, err := protector.NewExistingFixture(m.KeyFile)
	if err != nil {
		return out, err
	}
	if err = application.ValidateRestoreState(state, p, application.FixtureProfiles()); err != nil {
		return out, err
	}
	// The reference must identify the same material used for staged decryption.
	if current, err := fingerprint(m.KeyFile); err != nil || current != fp {
		return out, errors.New("independent key changed")
	}
	manifest := Manifest{Version: 1, Schema: sqlite.SchemaVersion, Mode: "fixture", Config: ConfigBinding, Build: buildinfo.Version, KeyFingerprint: fp, KeyVersion: 1, Revision: revision, AuditGap: gap || state.AuditGap, CreatedAt: created, Files: map[string]string{}}
	for _, name := range []string{"business.sqlite", "audit.sqlite"} {
		hash, err := digest(filepath.Join(stage, name))
		if err != nil {
			return out, err
		}
		manifest.Files[name] = hash
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return out, err
	}
	if err = write(filepath.Join(stage, "manifest.json"), raw); err != nil {
		return out, err
	}
	if err = syncDir(stage); err != nil {
		return out, err
	}
	backupID := "backup-" + rand.Text()
	final := filepath.Join(m.Root, backupID)
	if _, err = os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		return out, errors.New("backup already exists")
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if !allowed() {
		return out, domain.ErrPermission
	}
	if current, err := fingerprint(m.KeyFile); err != nil || current != fp {
		return out, errors.New("independent key changed")
	}
	if err = publish(stage, final); err != nil {
		return out, err
	}
	if err = syncDir(m.Root); err != nil {
		return out, err
	}
	return v1.BackupResult{BackupID: backupID, Revision: revision, AuditGap: manifest.AuditGap, CreatedAt: created, Mode: "fixture"}, nil
}
func (m *Manager) Retention(ctx context.Context, now time.Time, allowed func() bool) (v1.RetentionResult, error) {
	finish := m.beginForeground()
	defer finish()
	n, cutoff, err := m.Audit.RetainAuthorized(ctx, now, allowed)
	return v1.RetentionResult{Removed: n, Cutoff: cutoff.Format(time.RFC3339Nano), Mode: "fixture"}, err
}

// Validate verifies the complete fixed file set and bindings before the caller
// creates a restore directory. It does not generate or copy key material.
func Validate(ctx context.Context, source, keyFile string) (Manifest, application.State, error) {
	fail := func() (Manifest, application.State, error) {
		return Manifest{}, application.State{}, errors.New("backup validation rejected")
	}
	if SafePath(source) != nil {
		return fail()
	}
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return fail()
	}
	entries, err := os.ReadDir(source)
	if err != nil || len(entries) != 3 {
		return fail()
	}
	for _, entry := range entries {
		name := entry.Name()
		if name != "manifest.json" && name != "business.sqlite" && name != "audit.sqlite" {
			return fail()
		}
		info, err := os.Lstat(filepath.Join(source, name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return fail()
		}
	}
	f, err := os.Open(filepath.Join(source, "manifest.json"))
	if err != nil {
		return fail()
	}
	var manifest Manifest
	raw, readErr := io.ReadAll(io.LimitReader(f, 65537))
	err = v1.DecodeStrict(bytes.NewReader(raw), &manifest)
	f.Close()
	if err != nil || readErr != nil || len(raw) > 65536 || v1.RequireFields(raw, "version", "schema", "mode", "config", "build", "key_fingerprint", "key_version", "revision", "audit_gap", "created_at", "files") != nil {
		return fail()
	}
	if manifest.Version != 1 || manifest.Schema != sqlite.SchemaVersion || manifest.Mode != "fixture" || manifest.Config != ConfigBinding || manifest.Build != buildinfo.Version || manifest.KeyVersion != 1 || manifest.Revision < 0 || len(manifest.Files) != 2 {
		return fail()
	}
	at, err := time.Parse(time.RFC3339Nano, manifest.CreatedAt)
	if err != nil || at.IsZero() || at.After(time.Now().UTC()) || at.Location() != time.UTC {
		return fail()
	}
	fp, err := fingerprint(keyFile)
	if err != nil || fp != manifest.KeyFingerprint {
		return fail()
	}
	for _, name := range []string{"business.sqlite", "audit.sqlite"} {
		hash, err := digest(filepath.Join(source, name))
		if err != nil || hash != manifest.Files[name] {
			return fail()
		}
	}
	state, err := sqlite.Inspect(ctx, filepath.Join(source, "business.sqlite"), false)
	if err != nil || state.Revision != manifest.Revision || state.AuditGap && !manifest.AuditGap {
		return fail()
	}
	if _, err = sqlite.Inspect(ctx, filepath.Join(source, "audit.sqlite"), true); err != nil {
		return fail()
	}
	p, err := protector.NewExistingFixture(keyFile)
	if err != nil {
		return fail()
	}
	if application.ValidateRestoreState(state, p, application.FixtureProfiles()) != nil {
		return fail()
	}
	return manifest, state, nil
}
func Restore(ctx context.Context, source, target, keyFile string) error {
	if SafePath(target) != nil {
		return errors.New("unsafe restore target")
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return errors.New("restore target exists")
	}
	manifest, state, err := Validate(ctx, source, keyFile)
	if err != nil {
		return err
	}
	parent := filepath.Dir(target)
	stage, err := os.MkdirTemp(parent, ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, name := range []string{"business.sqlite", "audit.sqlite"} {
		// Recheck the hash of bytes actually copied, avoiding source replacement races.
		f, err := os.Open(filepath.Join(source, name))
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(filepath.Join(stage, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			f.Close()
			return err
		}
		h := sha256.New()
		_, err = io.Copy(io.MultiWriter(dst, h), f)
		f.Close()
		if err == nil {
			err = dst.Sync()
		}
		closeErr := dst.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if hex.EncodeToString(h.Sum(nil)) != manifest.Files[name] {
			return errors.New("restore source changed")
		}
	}
	// Verify again against the matching existing key and immutable staged bytes.
	p, err := protector.NewExistingFixture(keyFile)
	if err != nil {
		return err
	}
	staged, err := sqlite.Inspect(ctx, filepath.Join(stage, "business.sqlite"), false)
	if err != nil || application.ValidateRestoreState(staged, p, application.FixtureProfiles()) != nil {
		return errors.New("restore binding rejected")
	}
	state.RecoveryClosed = true
	state.AuditGap = manifest.AuditGap
	state.Revision++
	repo, err := sqlite.New(filepath.Join(stage, "business.sqlite"))
	if err != nil {
		return err
	}
	err = repo.Save(ctx, state)
	closeErr := repo.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = syncDir(stage); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err = os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return errors.New("restore target exists")
	}
	if err = publish(stage, target); err != nil {
		return err
	}
	return syncDir(parent)
}
