//go:build !windows

package daemon

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

type fileLock struct{ f *os.File }

// tryLock takes an exclusive, non-blocking lock on path.
func tryLock(path string) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return &fileLock{f: f}, nil
}

func (l *fileLock) unlock() {
	unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	l.f.Close()
}
