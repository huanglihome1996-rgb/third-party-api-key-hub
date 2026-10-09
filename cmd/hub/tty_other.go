//go:build !linux

package main

import (
	"context"
	"errors"
	"io"
)

func hiddenValue(io.Writer) (string, error) { return "", errors.New("Linux TTY required") }
func hiddenValueContext(context.Context, io.Writer) (string, error) {
	return "", errors.New("Linux TTY required")
}
