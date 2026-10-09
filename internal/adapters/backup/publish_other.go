//go:build !linux

package backup

import "errors"

func publish(string, string) error { return errors.New("Linux required") }
