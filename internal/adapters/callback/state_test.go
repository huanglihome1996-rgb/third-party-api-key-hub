package callback

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateStoreBindsAppAndAllowsOneConsume(t *testing.T) {
	store := NewStateStore(time.Minute)
	state, err := store.Issue("app-a")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := store.Consume(state); !ok || got != "app-a" {
		t.Fatalf("consume = %q, %v", got, ok)
	}
	if _, ok := store.Consume(state); ok {
		t.Fatal("state was reusable")
	}
}

func TestStateStoreExpires(t *testing.T) {
	store := NewStateStore(time.Nanosecond)
	state, err := store.Issue("app-a")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, ok := store.Consume(state); ok {
		t.Fatal("expired state was accepted")
	}
}

func TestStateStoreUsesDefaultTTL(t *testing.T) {
	store := NewStateStore(0)
	state, err := store.Issue("app-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Consume(state); !ok {
		t.Fatal("default TTL state was not accepted")
	}
}

func TestFileStateStoreSurvivesRestartWithoutWritingRawState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "states.json")
	first, err := NewFileStateStore(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	state, err := first.Issue("app-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" || string(b) == state {
		t.Fatal("state file is empty or contains the raw state")
	}
	second, err := NewFileStateStore(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if appID, ok := second.Consume(state); !ok || appID != "app-a" {
		t.Fatalf("restored state = %q, %v", appID, ok)
	}
}

func TestFileStateStoreRejectsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewFileStateStore("", time.Minute); err == nil {
		t.Fatal("accepted empty state path")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStateStore(bad, time.Minute); err == nil {
		t.Fatal("accepted malformed state file")
	}
	invalidEntry := filepath.Join(dir, "invalid-entry.json")
	if err := os.WriteFile(invalidEntry, []byte(`[{"hash":"00","app_id":"a"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStateStore(invalidEntry, time.Minute); err == nil {
		t.Fatal("accepted invalid state entry")
	}
}
