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
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCallbackHelperProcess$", "--",
		"-config", configPath, "-tls-cert", certPath, "-tls-key", keyPath)
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
		"apps": map[string]any{"test-app": map[string]string{
			"app_key":        "fixture-app-key",
			"app_secret_env": "TT_CALLBACK_TEST_APP_SECRET",
			"authorize_url":  "https://example.invalid/authorize",
			"token_url":      "https://example.invalid/token",
			"redirect_url":   "https://example.invalid/callbacks/tiktok-shop",
		}},
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
