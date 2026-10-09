//go:build !linux

package local

import (
	"errors"
	"github.com/third-party-api-key-hub/internal/application"
	"net"
)

func Peer(net.Conn) (application.Actor, error) {
	return application.Actor{}, errors.New("Linux peer credentials required")
}
