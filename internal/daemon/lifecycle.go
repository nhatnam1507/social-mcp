package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/control"
	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

// Start launches `<exe> daemon run --detached` in the background with output appended to
// daemon.log, then waits for it to answer on the control socket.
func Start(home string, timeout time.Duration) (service.StatusView, error) {
	client := control.NewClient(paths.SocketPath(home))
	if st, err := client.Status(); err == nil {
		return st, ErrAlreadyRunning
	}
	if _, err := paths.Ensure(home); err != nil {
		return service.StatusView{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return service.StatusView{}, err
	}
	logf, err := os.OpenFile(paths.Log(home), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return service.StatusView{}, err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "daemon", "run", "--detached")
	cmd.Env = append(os.Environ(), paths.EnvHome+"="+home)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := startDetached(cmd); err != nil {
		return service.StatusView{}, fmt.Errorf("start daemon: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.After(timeout)
	for {
		if st, err := client.Status(); err == nil {
			return st, nil
		}
		select {
		case err := <-exited:
			return service.StatusView{}, fmt.Errorf("daemon exited during startup (%v); last log lines:\n%s", err, tail(paths.Log(home), 15))
		case <-deadline:
			return service.StatusView{}, fmt.Errorf("daemon did not answer within %s; see %s", timeout, paths.Log(home))
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Stop asks the daemon to exit and waits until its process lock is released. If the daemon
// does not exit in time, the pidfile process is terminated while it still holds the lock.
func Stop(home string, timeout time.Duration) (forced bool, err error) {
	client := control.NewClient(paths.SocketPath(home))
	shutdownErr := client.Shutdown()
	if errors.Is(shutdownErr, control.ErrDaemonNotRunning) && !locked(home) {
		return false, control.ErrDaemonNotRunning
	}
	if waitUnlocked(home, timeout) {
		return false, nil
	}
	pid, perr := readPID(paths.PID(home))
	if perr != nil {
		return false, fmt.Errorf("daemon did not exit and its pid is unknown: %w", perr)
	}
	if !locked(home) {
		return false, nil
	}
	if err := signalTerminate(pid); err != nil {
		return false, fmt.Errorf("terminate daemon pid %d: %w", pid, err)
	}
	if !waitUnlocked(home, 5*time.Second) {
		return true, fmt.Errorf("daemon pid %d is still running", pid)
	}
	return true, nil
}

// Running reports whether a daemon holds the lock for home.
func Running(home string) bool { return locked(home) }

func locked(home string) bool {
	l, err := tryLock(paths.Lock(home))
	if err != nil {
		return errors.Is(err, errLocked)
	}
	l.unlock()
	return false
}

func waitUnlocked(home string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !locked(home) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func writePID(path string) error {
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
}

func readPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
