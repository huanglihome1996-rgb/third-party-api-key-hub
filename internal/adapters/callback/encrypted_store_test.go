package callback

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedFileTokenSinkDoesNotWritePlaintext(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "store.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("k", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tokens.enc.json")
	sink, err := NewEncryptedFileTokenSink(path, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Save(context.Background(), "app-a", Token{AccessToken: "access-secret", RefreshToken: "refresh-secret"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "access-secret") || strings.Contains(string(b), "refresh-secret") {
		t.Fatal("token store contains plaintext token")
	}
	reloaded, err := NewEncryptedFileTokenSink(path, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	values, err := reloaded.readLocked()
	if err != nil || values["app-a"].AccessToken != "access-secret" {
		t.Fatalf("reload = %#v, %v", values, err)
	}
}

func TestEncryptedFileTokenSinkRejectsBadKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "bad.key")
	if err := os.WriteFile(keyPath, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEncryptedFileTokenSink(filepath.Join(dir, "tokens"), keyPath); err == nil {
		t.Fatal("accepted short encryption key")
	}
}
