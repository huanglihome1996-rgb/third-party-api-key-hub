package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/third-party-api-key-hub/internal/adapters/callback"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "event=callback_startup_failed count=1")
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "callback JSON configuration")
	httpMode := flag.Bool("http", false, "serve plain HTTP behind an ALB that terminates external HTTPS")
	certPath := flag.String("tls-cert", "", "TLS certificate PEM")
	keyPath := flag.String("tls-key", "", "TLS private key PEM")
	flag.Parse()
	if flag.NArg() != 0 || *configPath == "" {
		return fmt.Errorf("config is required and positional arguments are not accepted")
	}
	if *httpMode && (*certPath != "" || *keyPath != "") {
		return fmt.Errorf("http cannot be combined with tls-cert or tls-key")
	}
	if !*httpMode && (*certPath == "" || *keyPath == "") {
		return fmt.Errorf("tls-cert and tls-key are required unless http is enabled")
	}
	configFile, err := os.Open(*configPath)
	if err != nil {
		return err
	}
	cfg, err := callback.LoadConfig(configFile, nil)
	closeErr := configFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	sink, err := callback.NewEncryptedFileTokenSink(cfg.TokenStorePath, cfg.TokenStoreKeyFile)
	if err != nil {
		return err
	}
	states, err := callback.NewFileStateStore(cfg.StateStorePath, 5*time.Minute)
	if err != nil {
		return err
	}
	handler, err := callback.NewHandler(callback.HandlerConfig{
		Apps:          cfg.Apps,
		States:        states,
		Exchanger:     callback.HTTPTokenExchanger{},
		Sink:          sink,
		AuthorizePath: cfg.AuthorizePath,
		CallbackPath:  cfg.CallbackPath,
	})
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() {
		if *httpMode {
			serveErr <- server.Serve(listener)
			return
		}
		serveErr <- server.ServeTLS(listener, *certPath, *keyPath)
	}()
	select {
	case err := <-serveErr:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-serveErr; err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
