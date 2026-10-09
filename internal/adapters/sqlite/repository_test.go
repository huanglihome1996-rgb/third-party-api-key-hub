package sqlite

import (
	"context"
	"github.com/third-party-api-key-hub/internal/application"
	"github.com/third-party-api-key-hub/internal/domain"
	"path/filepath"
	"testing"
)

func TestRepositoryCASRestartAndCancelledCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "business.sqlite")
	r, err := New(path)
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
	if err = r.Save(context.Background(), s); err == nil {
		t.Fatal("stale revision accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Revision = 2
	s.Apps["b"] = domain.App{ID: "b"}
	if err = r.Save(ctx, s); err == nil {
		t.Fatal("cancelled transaction accepted")
	}
	r.Close()
	r, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	loaded, err := r.Load(context.Background())
	if err != nil || loaded.Revision != 1 || loaded.NextOperation != 1 || len(loaded.Apps) != 1 {
		t.Fatal("invalid restart state", err)
	}
}
func TestSchemaMismatchFailsStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "business.sqlite")
	r, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.db.Exec("UPDATE metadata SET version=999"); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if r, err = New(path); err == nil {
		r.Close()
		t.Fatal("schema mismatch accepted")
	}
}
