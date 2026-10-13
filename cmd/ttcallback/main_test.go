//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestCallbackExitsOnTLSStartupFailure(t *testing.T) {
	for _, invalidCert := range []bool{false, true} {
		name := "missing_certificate"
		if invalidCert {
			name = "invalid_certificate"
		}
		t.Run(name, func(t *testing.T) {
			configPath := callbackTestConfig(t, "127.0.0.1:0")
			certPath := filepath.Join(filepath.Dir(configPath), "private-certificate.pem")
			if invalidCert {
				if err := os.WriteFile(certPath, []byte("invalid private certificate content"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := callbackTestProcess(ctx, configPath, certPath, certPath)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("callback remained running after TLS startup failed")
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("startup failure exit = %v, want exit status 1", err)
			}
			if string(output) != "event=callback_startup_failed count=1\n" {
				t.Fatalf("startup diagnostics must contain only the generic event, got %q", output)
			}
		})
	}
}

func TestCallbackRejectsInvalidTransportFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "TLS_requires_both_certificate_flags"},
		{name: "TLS_requires_key", args: []string{"-tls-cert", "private-certificate.pem"}},
		{name: "HTTP_rejects_certificate", args: []string{"-http", "-tls-cert", "private-certificate.pem"}},
		{name: "HTTP_rejects_key", args: []string{"-http", "-tls-key", "private-key.pem"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			configPath := callbackTestConfig(t, "127.0.0.1:0")
			cmd := callbackTestArgsProcess(ctx, append([]string{"-config", configPath}, tc.args...)...)
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("invalid transport flags must exit with status 1, got %v", err)
			}
			if string(output) != "event=callback_startup_failed count=1\n" {
				t.Fatalf("startup diagnostics must contain only the generic event, got %q", output)
			}
		})
	}
}

func TestCallbackHTTPBehindALBGracefulSIGTERM(t *testing.T) {
	portReservation := httptest.NewServer(http.NotFoundHandler())
	configPath := callbackTestConfig(t, portReservation.Listener.Addr().String())
	baseURL := portReservation.URL
	portReservation.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := callbackTestArgsProcess(ctx, "-config", configPath, "-http")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("HTTP health status = %d, want 200", resp.StatusCode)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("callback exited before HTTP became ready: %v", err)
		case <-ctx.Done():
			t.Fatal("HTTP startup deadline exceeded")
		case <-ticker.C:
		}
	}
	states := make(map[string]bool)
	for _, appID := range []string{"test-app", "other-app"} {
		resp, err := client.Get(baseURL + "/authorize?app_id=" + appID)
		if err != nil {
			t.Fatal(err)
		}
		var authorization struct {
			AppID            string `json:"app_id"`
			AuthorizationURL string `json:"authorization_url"`
		}
		err = json.NewDecoder(resp.Body).Decode(&authorization)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || authorization.AppID != appID {
			t.Fatalf("HTTP authorization for %s = %+v, status %d, error %v", appID, authorization, resp.StatusCode, err)
		}
		u, err := url.Parse(authorization.AuthorizationURL)
		if err != nil {
			t.Fatal(err)
		}
		query := u.Query()
		if query.Get("app_key") != "fixture-"+appID+"-key" || query.Get("redirect_uri") != "https://example.invalid/callbacks/tiktok-shop" {
			t.Fatalf("HTTP upstream must retain configured app and external HTTPS callback, got %q", authorization.AuthorizationURL)
		}
		state := query.Get("state")
		if state == "" || states[state] || query.Has("app_secret") {
			t.Fatal("authorization must issue distinct state and omit app secret")
		}
		states[state] = true
	}
	resp, err := client.Get(baseURL + "/callbacks/tiktok-shop?code=fixture-code")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("HTTP callback without state = %d, want 400", resp.StatusCode)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("HTTP SIGTERM should exit successfully: %v", err)
		}
		if output.Len() != 0 {
			t.Fatalf("unexpected HTTP shutdown diagnostics: %q", output.String())
		}
	case <-ctx.Done():
		t.Fatal("HTTP graceful shutdown deadline exceeded")
	}
}

func TestCallbackHTTPSGracefulSIGTERM(t *testing.T) {
	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	configPath := callbackTestConfig(t, certificateServer.Listener.Addr().String())
	certPath := filepath.Join(filepath.Dir(configPath), "server.pem")
	keyPath := filepath.Join(filepath.Dir(configPath), "server.key")
	certificate := certificateServer.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	client := certificateServer.Client()
	client.Timeout = time.Second
	defer client.CloseIdleConnections()
	certificateServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := callbackTestProcess(ctx, configPath, certPath, keyPath)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		resp, err := client.Get(certificateServer.URL + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("HTTPS health status = %d, want 200", resp.StatusCode)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("callback exited before HTTPS became ready: %v", err)
		case <-ctx.Done():
			t.Fatal("HTTPS startup deadline exceeded")
		case <-ticker.C:
		}
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SIGTERM should exit successfully: %v", err)
		}
		if output.Len() != 0 {
			t.Fatalf("unexpected shutdown diagnostics: %q", output.String())
		}
	case <-ctx.Done():
		t.Fatal("graceful shutdown deadline exceeded")
	}
}

func TestCallbackHelperProcess(t *testing.T) {
	if os.Getenv("TT_CALLBACK_TEST_PROCESS") != "1" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("ttcallback", flag.ExitOnError)
	os.Args = append([]string{"ttcallback"}, os.Args[3:]...)
	main()
	os.Exit(0)
}

func callbackTestProcess(ctx context.Context, configPath, certPath, keyPath string) *exec.Cmd {
	return callbackTestArgsProcess(ctx, "-config", configPath, "-tls-cert", certPath, "-tls-key", keyPath)
}

func callbackTestArgsProcess(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCallbackHelperProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "TT_CALLBACK_TEST_PROCESS=1", "TT_CALLBACK_TEST_APP_SECRET=fixture-app-secret")
	return cmd
}

func callbackTestConfig(t *testing.T, listenAddr string) string {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "token-store.key")
	if err := os.WriteFile(keyPath, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{
		"listen_addr":          listenAddr,
		"state_store_path":     filepath.Join(dir, "states.json"),
		"token_store_path":     filepath.Join(dir, "tokens.enc.json"),
		"token_store_key_file": keyPath,
		"apps":                 map[string]any{},
	}
	for _, appID := range []string{"test-app", "other-app"} {
		config["apps"].(map[string]any)[appID] = map[string]string{
			"app_key":        "fixture-" + appID + "-key",
			"app_secret_env": "TT_CALLBACK_TEST_APP_SECRET",
			"authorize_url":  "https://example.invalid/authorize",
			"token_url":      "https://example.invalid/token",
			"redirect_url":   "https://example.invalid/callbacks/tiktok-shop",
		}
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	return configPath
}
