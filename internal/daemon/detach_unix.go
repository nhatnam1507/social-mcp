//go:build !windows

package daemon

import (
	"os/exec"
	"syscall"
)

func startDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func signalTerminate(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
