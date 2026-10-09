//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/third-party-api-key-hub/internal/adapters/local"
	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/adapters/sqlite"
	"github.com/third-party-api-key-hub/internal/application"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIUnixSQLiteLifecycleAndRestart(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "hub.sock")
	start := func() func() {
		repo, err := sqlite.New(filepath.Join(dir, "business.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		audit, err := sqlite.NewAudit(filepath.Join(dir, "audit.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		p, err := protector.NewFixture(filepath.Join(dir, "fixture.key"))
		if err != nil {
			t.Fatal(err)
		}
		authorize := func(a application.Actor) application.Actor {
			a.Read = a.UID == uint32(os.Getuid())
			a.Manage = a.Read
			return a
		}
		engine, err := application.NewEngine(repo, p, audit, authorize)
		if err != nil {
			t.Fatal(err)
		}
		l, err := local.Listen(socket)
		if err != nil {
			t.Fatal(err)
		}
		srv := local.Server(engine, authorize)
		done := make(chan struct{})
		go func() { defer close(done); srv.Serve(l) }()
		return func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			srv.Shutdown(ctx)
			l.Close()
			<-done
			repo.Close()
			audit.Close()
		}
	}
	stop := start()
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	call := func(op string, payload any, wantCode int) []byte {
		raw, _ := json.Marshal(payload)
		req, _ := json.Marshal(v1.OperationRequest{Version: v1.Version, Operation: op, Payload: raw})
		var out bytes.Buffer
		code := run(bytes.NewReader(req), &out, socket)
		if code != wantCode {
			t.Fatalf("%s got code %d: %s", op, code, out.String())
		}
		return out.Bytes()
	}
	for _, entry := range []struct {
		op string
		p  any
	}{
		{"app.register", map[string]any{"app_id": "a", "platform": "fixture", "provider_app_id": "pa"}},
		{"shop.register", map[string]any{"external_shop_id": "s", "platform": "fixture", "provider_shop_id": "ps"}},
		{"brand.register", map[string]any{"brand_id": "b"}},
		{"internal_shop.register", map[string]any{"internal_shop_id": "i"}},
		{"shop.map_brand", map[string]any{"external_shop_id": "s", "brand_id": "b", "expected_revision": 1}},
		{"brand.map_internal_shop", map[string]any{"brand_id": "b", "internal_shop_id": "i", "expected_revision": 1}},
		{"installation.create", map[string]any{"app_id": "a", "external_shop_id": "s", "expected_revision": 1}},
	} {
		call(entry.op, entry.p, 0)
	}
	importValue := func(scope, typ, value string, replace, confirm bool, g int64) {
		p := map[string]any{"scope": scope, "app_id": "a", "type": typ, "value": value, "replace": replace, "expiry_known": false, "expires_at": nil, "confirm": confirm}
		if scope == "shop" {
			p["external_shop_id"] = "s"
		}
		if replace {
			p["expected_generation"] = g
		}
		call("credential.import", p, 0)
	}
	importValue("app", "client_secret", "fixture-app", false, true, 0)
	importValue("shop", "shop_token", "fixture-shop", false, true, 0)
	importValue("shop", "refresh_token", "fixture-management", false, true, 0)
	query := map[string]any{"by": "external_shop", "external_shop_id": "s"}
	checkQuery := func() {
		r := call("credential.query", query, 0)
		if !bytes.Contains(r, []byte("fixture-app")) || !bytes.Contains(r, []byte("fixture-shop")) || bytes.Contains(r, []byte("fixture-management")) {
			t.Fatal("invalid business combination")
		}
	}
	checkQuery()
	for _, op := range []string{"health", "app.list", "shop.list", "brand.list", "internal_shop.list", "installation.list", "credential.status", "audit.list"} {
		r := call(op, map[string]any{}, 0)
		if bytes.Contains(r, []byte("fixture-app")) || bytes.Contains(r, []byte("fixture-shop")) {
			t.Fatal("metadata leaked secret", op)
		}
	}
	stop()
	stop = nil
	stop = start()
	checkQuery()
	ref := map[string]any{"scope": "app", "app_id": "a", "type": "client_secret"}
	call("credential.disable", ref, 0)
	r := call("credential.query", query, 1)
	if strings.Contains(string(r), "fixture-") || !bytes.Contains(r, []byte("credential_disabled")) {
		t.Fatal("partial failure leaked")
	}
	importValue("app", "client_secret", "fixture-app-new", true, false, 1)
	ref["expected_generation"] = 2
	call("credential.restore", ref, 1)
	call("credential.confirm", ref, 0)
	call("credential.restore", ref, 0)
	stale := map[string]any{"scope": "app", "app_id": "a", "type": "client_secret", "expected_generation": 1, "reason": "invalid"}
	call("credential.mark_invalid", stale, 1)
	call("credential.query", query, 0)
	metadata := map[string]any{"scope": "app", "app_id": "a", "type": "client_secret", "replace": true, "expected_generation": 2, "expiry_known": false, "expires_at": nil, "confirm": true}
	payload, _ := json.Marshal(metadata)
	interactiveRequest, _ := json.Marshal(v1.OperationRequest{Version: v1.Version, Operation: "credential.import", Payload: payload})
	_, code := interactiveCall(t, socket, string(interactiveRequest), "fixture-interactive", true)
	if code != 1 {
		t.Fatal("cancel import succeeded")
	}
	r = call("credential.query", query, 0)
	if !bytes.Contains(r, []byte("fixture-app-new")) {
		t.Fatal("cancel changed credential")
	}
	// A line beyond the canonical terminal limit must arrive complete in noncanonical mode.
	longValue := "fixture-" + strings.Repeat("x", 5000)
	_, code = interactiveCall(t, socket, string(interactiveRequest), longValue, false)
	if code != 0 {
		t.Fatal("interactive import failed")
	}
	r = call("credential.query", query, 0)
	if !bytes.Contains(r, []byte(longValue)) {
		t.Fatal("hidden input was truncated")
	}
	call("shop.unmap_brand", map[string]any{"external_shop_id": "s", "expected_revision": 2}, 0)
	call("credential.query", query, 1)
}
