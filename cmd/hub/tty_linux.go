//go:build linux

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unicode/utf8"
)

func hiddenValue(diagnostics io.Writer) (string, error) {
	return hiddenValueContext(context.Background(), diagnostics)
}
func hiddenValueContext(parent context.Context, diagnostics io.Writer) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", errors.New("TTY required")
	}
	defer tty.Close()
	old, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
	if err != nil {
		return "", errors.New("TTY unavailable")
	}
	state := *old
	state.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON
	state.Cc[unix.VMIN] = 1
	state.Cc[unix.VTIME] = 0
	if err = unix.IoctlSetTermios(int(tty.Fd()), unix.TCSETS, &state); err != nil {
		return "", errors.New("TTY unavailable")
	}
	defer unix.IoctlSetTermios(int(tty.Fd()), unix.TCSETSF, old)
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	fmt.Fprint(diagnostics, "Fixture value (hidden): ")
	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(tty)
		value := make([]byte, 0, 256)
		for {
			b, err := reader.ReadByte()
			if err != nil {
				done <- result{"", err}
				return
			}
			switch b {
			case '\r', '\n':
				done <- result{string(value), nil}
				return
			case 4:
				done <- result{"", errors.New("input cancelled")}
				return
			case 8, 127:
				if len(value) > 0 {
					_, n := utf8.DecodeLastRune(value)
					value = value[:len(value)-n]
				}
			default:
				if len(value) >= 65536 {
					done <- result{"", errors.New("input too large")}
					return
				}
				value = append(value, b)
			}
		}
	}()
	select {
	case r := <-done:
		fmt.Fprintln(diagnostics)
		if r.err != nil || r.value == "" {
			return "", errors.New("input cancelled")
		}
		return r.value, nil
	case <-ctx.Done():
		fmt.Fprintln(diagnostics)
		return "", errors.New("input cancelled")
	}
}
