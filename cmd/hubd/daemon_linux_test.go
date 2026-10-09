//go:build linux

package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDaemonFixturePersistenceAndGracefulRestart(t *testing.T) {
	oldFlags, oldArgs := flag.CommandLine, os.Args
	defer func() { flag.CommandLine, os.Args = oldFlags, oldArgs }()
	configure := func(args ...string) {
		flag.CommandLine = flag.NewFlagSet("hubd-test", flag.ContinueOnError)
		flag.CommandLine.SetOutput(io.Discard)
		os.Args = append([]string{"hubd-test"}, args...)
	}
	configure()
	if err := run(); err == nil {
		t.Fatal("production startup accepted without OpenBao")
	}
	dir := t.TempDir()
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "hub.sock"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	request := func(operation, payload string) (map[string]any, error) {
		r, err := client.Post("http://localhost/v1/operations", "application/json", strings.NewReader(`{"version":"contract/v1","operation":"`+operation+`","payload":`+payload+`}`))
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		var result map[string]any
		err = json.NewDecoder(r.Body).Decode(&result)
		return result, err
	}
	for restart := 0; restart < 2; restart++ {
		configure("--fixture", "--state-dir", dir)
		done := make(chan error, 1)
		go func() { done <- run() }()
		deadline := time.Now().Add(5 * time.Second)
		var health map[string]any
		for {
			var err error
			health, err = request("health", "{}")
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("fixture failed to start", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if health["mode"] != "fixture" || health["audit_degraded"] != false || health["revision"] != float64(restart) {
			t.Fatal("invalid restart health", health)
		}
		if restart == 0 {
			result, err := request("app.register", `{"app_id":"a","platform":"fixture","provider_app_id":"pa"}`)
			if err != nil || result["revision"] != float64(1) {
				t.Fatal("fixture mutation failed", result, err)
			}
		} else {
			result, err := request("app.list", "{}")
			if err != nil || result["total"] != float64(1) {
				t.Fatal("persisted registration missing", result, err)
			}
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal("graceful shutdown failed", err)
			}
		case <-time.After(6 * time.Second):
			t.Fatal("shutdown deadline exceeded")
		}
	}
}
