//go:build !windows

package daemon

import (
	"os/exec"
	"syscall"
)

// errAddrInUse is the errno for a port another process owns.
var errAddrInUse error = syscall.EADDRINUSE

func startDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func signalTerminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}

func signalKill(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}
