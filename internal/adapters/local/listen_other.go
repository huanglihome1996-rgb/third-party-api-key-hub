//go:build !linux

package local

import (
	"errors"
	"net"
)

func Listen(string) (net.Listener, error)        { return nil, errors.New("Linux required") }
func AcquireInstanceLock(string) (func(), error) { return nil, errors.New("Linux required") }
