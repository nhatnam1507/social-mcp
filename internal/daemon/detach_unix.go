//go:build !windows

package daemon

import (
	"errors"
	"os/exec"
	"syscall"
)

// errAddrInUse is the errno for a port another process owns.
var errAddrInUse error = syscall.EADDRINUSE

func startDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func signalTerminate(pid int) error { return signal(pid, syscall.SIGTERM) }

func signalKill(pid int) error { return signal(pid, syscall.SIGKILL) }

// signal treats a process that already exited as success.
func signal(pid int, sig syscall.Signal) error {
	if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
