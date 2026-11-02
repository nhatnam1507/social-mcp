package daemon

import (
	"errors"
	"fmt"
	"io"
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
	if st, err := client.StatusWithin(probeTimeout); err == nil {
		return st, ErrAlreadyRunning
	}
	if locked(home) {
		return service.StatusView{}, ErrUnresponsive
	}
	if _, err := paths.Ensure(home); err != nil {
		return service.StatusView{}, err
	}
	rotateLog(paths.Log(home))
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
	cmd.Dir = home // do not pin the caller's working directory
	cmd.Env = append(os.Environ(), paths.EnvHome+"="+home)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := startDetached(cmd); err != nil {
		return service.StatusView{}, fmt.Errorf("start daemon: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.After(timeout)
	for {
		if st, err := client.StatusWithin(probeTimeout); err == nil {
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
	// If the shutdown request could not be delivered, waiting is pointless: go straight to
	// terminating the lock holder.
	if shutdownErr == nil && waitUnlocked(home, timeout) {
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

// ErrUnresponsive means a daemon holds the lock but its control socket does not answer.
var ErrUnresponsive = errors.New("a daemon is running but its control socket does not answer; run `social-mcp daemon stop`")

const (
	probeTimeout = 2 * time.Second
	maxLogBytes  = 10 << 20
)

// rotateLog keeps one previous log once daemon.log exceeds maxLogBytes.
func rotateLog(path string) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogBytes {
		os.Rename(path, path+".1")
	}
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
	b, err := ReadTail(path, 64<<10)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ReadTail returns at most the last limit bytes of path.
func ReadTail(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(fi.Size()-limit, 0)
	buf := make([]byte, fi.Size()-start)
	_, err = f.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf, nil
}
