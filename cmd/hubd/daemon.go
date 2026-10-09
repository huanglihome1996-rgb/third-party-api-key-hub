package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/third-party-api-key-hub/internal/adapters/local"
	"github.com/third-party-api-key-hub/internal/adapters/protector"
	"github.com/third-party-api-key-hub/internal/adapters/sqlite"
	"github.com/third-party-api-key-hub/internal/application"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "service startup or runtime failed")
		os.Exit(1)
	}
}
func run() error {
	fixture := flag.Bool("fixture", false, "run isolated fictional local demonstration only")
	dir := flag.String("state-dir", "/tmp/api-key-hub-fixture", "isolated fixture state directory")
	flag.Parse()
	if !*fixture || runtime.GOOS != "linux" {
		return fmt.Errorf("production OpenBao and Linux deployment remain gated")
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(*dir, 0700); err != nil {
		return err
	}
	repo, err := sqlite.New(filepath.Join(*dir, "business.sqlite"))
	if err != nil {
		return err
	}
	defer repo.Close()
	audit, err := sqlite.NewAudit(filepath.Join(*dir, "audit.sqlite"))
	if err != nil {
		return err
	}
	defer audit.Close()
	p, err := protector.NewFixture(filepath.Join(*dir, "fixture.key"))
	if err != nil {
		return err
	}
	uid := uint32(os.Getuid())
	authorize := func(a application.Actor) application.Actor { a.Read = a.UID == uid; a.Manage = a.UID == uid; return a }
	engine, err := application.NewEngine(repo, p, audit, authorize)
	if err != nil {
		return err
	}
	listener, err := local.Listen(filepath.Join(*dir, "hub.sock"))
	if err != nil {
		return err
	}
	defer listener.Close()
	server := local.Server(engine, authorize)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		reported := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if engine.AuditDegraded() && !reported {
					fmt.Fprintln(os.Stderr, "audit degraded; inspect non-sensitive health status")
					reported = true
				}
			}
		}
	}()
	fmt.Fprintln(os.Stderr, "fixture service ready; external acceptance remains pending")
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
