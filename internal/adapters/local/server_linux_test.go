//go:build linux

package local

import (
	"context"
	"encoding/json"
	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/application"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type repository struct{}

func (repository) Load(context.Context) (application.State, error) {
	return application.EmptyState(), nil
}
func (repository) Save(context.Context, application.State) error { return nil }

type audit struct {
	mu     sync.Mutex
	events []application.AuditEvent
}

func (a *audit) Record(_ context.Context, e application.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
	return nil
}
func start(t *testing.T, authorize func(application.Actor) application.Actor) (string, *audit, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hub.sock")
	a := &audit{}
	engine, err := application.NewEngine(repository{}, protector.NewMemory(), a, authorize)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	server := Server(engine, authorize)
	done := make(chan struct{})
	go func() { defer close(done); server.Serve(listener) }()
	return path, a, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		server.Shutdown(ctx)
		listener.Close()
		<-done
	}
}
func request(path, body string) ([]byte, error) {
	tr := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer tr.CloseIdleConnections()
	client := http.Client{Transport: tr, Timeout: time.Second}
	r, _ := http.NewRequest("POST", "http://localhost/v1/operations", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	response, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}
func TestMissingIdentityNeverUsesUIDZero(t *testing.T) {
	a := &audit{}
	allow := func(x application.Actor) application.Actor { x.Manage = true; return x }
	engine, _ := application.NewEngine(repository{}, protector.NewMemory(), a, allow)
	server := Server(engine, allow)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve(listener)
	response, err := http.Post("http://"+listener.Addr().String()+"/v1/operations", "application/json", strings.NewReader(`{"version":"contract/v1","operation":"health","payload":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var failure v1.ErrorResponse
	if json.NewDecoder(response.Body).Decode(&failure) != nil || failure.Error != v1.ErrPermissionDenied {
		t.Fatal("missing identity allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.events) != 1 || a.events[0].UID != ^uint32(0) || a.events[0].ActorName != "unidentified" {
		t.Fatal("unidentified peer audit impersonated root")
	}
}
func TestBoundaryRejectionAudited(t *testing.T) {
	path, a, stop := start(t, func(x application.Actor) application.Actor { x.Manage = true; return x })
	defer stop()
	raw, err := request(path, `{"value":"fixture-do-not-log","value":"duplicate"}`)
	if err != nil || !strings.Contains(string(raw), "input_invalid") {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.events) != 1 || a.events[0].Operation != "local.reject" {
		t.Fatal("missing rejection audit")
	}
	b, _ := json.Marshal(a.events)
	if strings.Contains(string(b), "fixture-do-not-log") {
		t.Fatal("audit leak")
	}
}
func TestStaleSocketRecoveryAndLiveLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.sock")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	l.Close()
	live, err := Listen(path)
	if err != nil {
		t.Fatal("stale recovery", err)
	}
	defer live.Close()
	if second, err := Listen(path); err == nil {
		second.Close()
		t.Fatal("live service replaced")
	}
}
func TestDifferentUIDHelper(t *testing.T) {
	if os.Getenv("HUB_UID_HELPER") != "1" {
		return
	}
	raw, err := request(os.Getenv("HUB_UID_SOCKET"), `{"version":"contract/v1","operation":"health","payload":{}}`)
	if err != nil {
		os.Exit(2)
	}
	os.Stdout.Write(raw)
	os.Exit(0)
}
func TestDifferentPeerUIDRejected(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires root only to construct a different UID test process")
	}
	path, _, stop := start(t, func(x application.Actor) application.Actor { x.Read = x.UID == 0; x.Manage = x.UID == 0; return x })
	defer stop()
	os.Chmod(filepath.Dir(filepath.Dir(path)), 0755)
	os.Chmod(filepath.Dir(path), 0755)
	os.Chmod(path, 0666)
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(filepath.Dir(path), "peer-test")
	if err = os.WriteFile(helper, binary, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(helper, "-test.run=^TestDifferentUIDHelper$")
	cmd.Env = append(os.Environ(), "HUB_UID_HELPER=1", "HUB_UID_SOCKET="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var failure v1.ErrorResponse
	if json.Unmarshal(raw, &failure) != nil || failure.Error != v1.ErrPermissionDenied {
		t.Fatal("different UID impersonated service")
	}
}
