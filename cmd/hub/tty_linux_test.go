//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHiddenInputHelper(t *testing.T) {
	if os.Getenv("HUB_TTY_HELPER") != "1" {
		return
	}
	if os.Getenv("HUB_INTERACTIVE") != "" {
		os.Exit(runMode(strings.NewReader(os.Getenv("HUB_INTERACTIVE")), os.Stdout, os.Stderr, os.Getenv("HUB_INTERACTIVE_SOCKET"), true))
	}
	value, err := hiddenValue(os.Stderr)
	if err != nil {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"cancelled": true})
	} else {
		json.NewEncoder(os.Stdout).Encode(map[string]any{"matched": value == "fixture-hidden"})
	}
	os.Exit(0)
}

func interactiveCall(t *testing.T, socket, request, value string, cancelInput bool) ([]byte, int) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	if err = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	old, _ := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	command := exec.Command(os.Args[0], "-test.run=^TestHiddenInputHelper$")
	command.Env = append(os.Environ(), "HUB_TTY_HELPER=1", "HUB_INTERACTIVE="+request, "HUB_INTERACTIVE_SOCKET="+socket)
	command.Stderr = slave
	var out bytes.Buffer
	command.Stdout = &out
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 2}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	prompt := make(chan string, 1)
	go func() { b := make([]byte, 256); n, _ := master.Read(b); prompt <- string(b[:n]) }()
	select {
	case p := <-prompt:
		if !strings.Contains(p, "hidden") {
			t.Fatal("no hidden prompt")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prompt timeout")
	}
	if cancelInput {
		command.Process.Signal(os.Interrupt)
	} else {
		io.WriteString(master, value+"\n")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	code := 0
	select {
	case err := <-done:
		if err != nil {
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
	case <-time.After(10 * time.Second):
		t.Fatal("interactive timeout")
	}
	after, _ := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if after.Lflag != old.Lflag {
		t.Fatal("interactive did not restore TTY")
	}
	if bytes.Contains(out.Bytes(), []byte(value)) {
		t.Fatal("interactive secret echoed")
	}
	return out.Bytes(), code
}
func TestHiddenInputAndSignalRestore(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancel), func(t *testing.T) {
			master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			if err = unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|unix.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer slave.Close()
			before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(os.Args[0], "-test.run=^TestHiddenInputHelper$")
			command.Env = append(os.Environ(), "HUB_TTY_HELPER=1")
			command.Stderr = slave
			var output bytes.Buffer
			command.Stdout = &output
			command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 2}
			if err = command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if command.Process != nil {
					command.Process.Kill()
				}
			}()
			prompt := make(chan string, 1)
			go func() { b := make([]byte, 256); n, _ := master.Read(b); prompt <- string(b[:n]) }()
			select {
			case p := <-prompt:
				if !strings.Contains(p, "hidden") {
					t.Fatal("missing prompt")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("prompt timeout")
			}
			active, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil || active.Lflag&unix.ECHO != 0 {
				t.Fatal("echo not disabled")
			}
			if cancel {
				command.Process.Signal(os.Interrupt)
			} else {
				io.WriteString(master, "fixture-hidden\n")
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("TTY child timeout")
			}
			after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil || after.Lflag != before.Lflag {
				t.Fatal("TTY not restored")
			}
			if bytes.Contains(output.Bytes(), []byte("fixture-hidden")) {
				t.Fatal("hidden value leaked")
			}
			if cancel && !bytes.Contains(output.Bytes(), []byte("cancelled")) || !cancel && !bytes.Contains(output.Bytes(), []byte(`"matched":true`)) {
				t.Fatal("unexpected child result")
			}
		})
	}
}
