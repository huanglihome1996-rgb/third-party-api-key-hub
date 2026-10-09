package protector

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureBindingAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	p, err := NewFixture(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := p.Seal("current-credential", "app-1|", "client_secret", "fixture-value", 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "fixture-value") {
		t.Fatal("plaintext ciphertext")
	}
	q, err := NewFixture(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := q.Open("current-credential", "app-1|", "client_secret", sealed, 1)
	if err != nil || value != "fixture-value" {
		t.Fatal("restart failed", err)
	}
	for _, binding := range []struct {
		purpose, target, typ string
		g                    int64
	}{{"pending-provider-result", "app-1|", "client_secret", 1}, {"current-credential", "app-2|", "client_secret", 1}, {"current-credential", "app-1|", "shop_token", 1}, {"current-credential", "app-1|", "client_secret", 2}} {
		if _, err = q.Open(binding.purpose, binding.target, binding.typ, sealed, binding.g); err == nil {
			t.Fatal("wrong binding accepted")
		}
	}
}
func TestMemoryGenerationsStayIndependent(t *testing.T) {
	p := NewMemory()
	a, _ := p.Seal("current-credential", "app", "token", "fake-a", 1)
	b, _ := p.Seal("current-credential", "app", "token", "fake-b", 2)
	v, err := p.Open("current-credential", "app", "token", a, 1)
	if err != nil || v != "fake-a" || a == b {
		t.Fatal("overwritten generation")
	}
	if _, err = p.Open("current-credential", "app", "token", a, 2); err == nil {
		t.Fatal("generation accepted")
	}
}
