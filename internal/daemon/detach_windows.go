//go:build windows

package daemon

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func startDetached(cmd *exec.Cmd) error {
	base := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: base | windows.CREATE_BREAKAWAY_FROM_JOB, HideWindow: true}
	err := cmd.Start()
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// The parent job does not allow breakaway; start without it.
		retry := exec.Command(cmd.Path, cmd.Args[1:]...)
		retry.Env, retry.Stdin, retry.Stdout, retry.Stderr = cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr
		retry.SysProcAttr = &syscall.SysProcAttr{CreationFlags: base, HideWindow: true}
		*cmd = *retry
		return cmd.Start()
	}
	return err
}

func signalTerminate(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
