//go:build linux

package local

import (
	"errors"
	"github.com/third-party-api-key-hub/internal/application"
	"golang.org/x/sys/unix"
	"net"
)

func Peer(conn net.Conn) (application.Actor, error) {
	c, ok := conn.(*net.UnixConn)
	if !ok {
		return application.Actor{}, errors.New("Unix peer required")
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return application.Actor{}, err
	}
	var cred *unix.Ucred
	var peerErr error
	if err = raw.Control(func(fd uintptr) { cred, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return application.Actor{}, err
	}
	if peerErr != nil || cred == nil {
		return application.Actor{}, errors.New("peer unavailable")
	}
	return application.Actor{UID: cred.Uid}, nil
}
