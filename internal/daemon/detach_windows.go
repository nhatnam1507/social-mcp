//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// errAddrInUse is the Winsock error for a port another process owns.
var errAddrInUse error = windows.WSAEADDRINUSE

func startDetached(cmd *exec.Cmd) error {
	base := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: base | windows.CREATE_BREAKAWAY_FROM_JOB, HideWindow: true}
	err := cmd.Start()
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		// The parent job does not allow breakaway; start without it. The daemon then ends
		// when that job closes (for example an SSH session), so record why.
		if cmd.Stderr != nil {
			fmt.Fprintln(cmd.Stderr, "social-mcp: the parent job does not allow breakaway; the daemon will stop when the starting session's job ends")
		}
		retry := exec.Command(cmd.Path, cmd.Args[1:]...)
		retry.Dir = cmd.Dir
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
