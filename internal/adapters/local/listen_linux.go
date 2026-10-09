//go:build linux

package local

import (
	"errors"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

type lockedListener struct {
	net.Listener
	lock *os.File
	once sync.Once
}

// AcquireInstanceLock must precede all database/key initialization or restore.
func AcquireInstanceLock(path string) (func(), error) {
	info, err := os.Lstat(path)
	if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("unsafe lock path")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, errors.New("service already running")
	}
	var once sync.Once
	return func() { once.Do(func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }) }, nil
}

func (l *lockedListener) Close() error {
	var err error
	l.once.Do(func() { err = l.Listener.Close(); unix.Flock(int(l.lock.Fd()), unix.LOCK_UN); l.lock.Close() })
	return err
}
func Listen(path string) (net.Listener, error) {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	release := func() { unix.Flock(int(lock.Fd()), unix.LOCK_UN); lock.Close() }
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("service already running")
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Getuid()) || info.Mode()&os.ModeSocket == 0 {
			release()
			return nil, errors.New("unsafe existing socket path")
		}
		conn, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			release()
			return nil, errors.New("service already running")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) {
			release()
			return nil, errors.New("socket state uncertain")
		}
		if err = os.Remove(path); err != nil {
			release()
			return nil, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		release()
		return nil, statErr
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		release()
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		listener.Close()
		release()
		return nil, err
	}
	return &lockedListener{Listener: listener, lock: lock}, nil
}
