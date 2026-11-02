//go:build !windows

package videofile

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestFIFORejectedWithoutBlocking(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pipe.mp4")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip("mkfifo unsupported:", err)
	}
	done := make(chan error, 1)
	go func() { _, err := Check(p, nil); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Check blocked on a FIFO")
	}
}
