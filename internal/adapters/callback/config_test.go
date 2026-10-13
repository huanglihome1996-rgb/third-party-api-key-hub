package callback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigResolvesMultipleAppsFromSeparateSecretFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.secret"), []byte("secret-a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.secret"), []byte("secret-b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	raw := `{"listen_addr":"127.0.0.1:9080","callback_path":"/callbacks/tiktok-shop","apps":{"app-a":{"app_key":"key-a","app_secret_file":"` + filepath.ToSlash(filepath.Join(dir, "a.secret")) + `","authorize_url":"https://auth.example/a","token_url":"https://token.example/a","redirect_url":"https://callback.example/a"},"app-b":{"app_key":"key-b","app_secret_file":"` + filepath.ToSlash(filepath.Join(dir, "b.secret")) + `","authorize_url":"https://auth.example/b","token_url":"https://token.example/b","redirect_url":"https://callback.example/b"}}}`
	cfg, err := LoadConfig(strings.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Apps["app-a"].AppKey != "key-a" || cfg.Apps["app-a"].AppSecret != "secret-a" {
		t.Fatalf("app-a not resolved: %#v", cfg.Apps["app-a"])
	}
	if cfg.Apps["app-b"].AppKey != "key-b" || cfg.Apps["app-b"].AppSecret != "secret-b" {
		t.Fatalf("app-b not resolved: %#v", cfg.Apps["app-b"])
	}
}

func TestLoadConfigRejectsInlineSecretAndDuplicateSecretSources(t *testing.T) {
	for name, raw := range map[string]string{
		"inline":    `{"apps":{"a":{"app_key":"k","app_secret":"do-not-allow"}}}`,
		"duplicate": `{"apps":{"a":{"app_key":"k","app_secret_env":"A","app_secret_file":"/tmp/a"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(strings.NewReader(raw), map[string]string{"A": "secret"}); err == nil {
				t.Fatal("expected invalid secret configuration")
			}
		})
	}
}

func TestLoadConfigResolvesEnvironmentSecretAndRejectsInvalidURLs(t *testing.T) {
	raw := `{"apps":{"a":{"app_key":"k","app_secret_env":"A","authorize_url":"https://a","token_url":"https://t","redirect_url":"https://r"}}}`
	cfg, err := LoadConfig(strings.NewReader(raw), map[string]string{"A": "secret"})
	if err != nil || cfg.Apps["a"].AppSecret != "secret" {
		t.Fatalf("env secret = %#v, %v", cfg, err)
	}
	for _, bad := range []string{
		`{"authorize_path":"/same","callback_path":"/same","apps":{"a":{"app_key":"k","app_secret_env":"A","authorize_url":"https://a","token_url":"https://t","redirect_url":"https://r"}}}`,
		`{"apps":{"a":{"app_key":"k","app_secret_env":"A","authorize_url":"ftp://a","token_url":"https://t","redirect_url":"https://r"}}}`,
	} {
		if _, err := LoadConfig(strings.NewReader(bad), map[string]string{"A": "secret"}); err == nil {
			t.Fatal("invalid config was accepted")
		}
	}
	if _, err := LoadConfig(strings.NewReader(raw), map[string]string{}); err == nil {
		t.Fatal("missing environment secret was accepted")
	}
}
