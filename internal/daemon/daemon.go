// Package daemon runs the long-lived social-mcp process: control socket, loopback HTTP
// listener (OAuth callback and MCP), upload runner and token refresher.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/config"
	"github.com/nhatnam1507/social-mcp/internal/control"
	"github.com/nhatnam1507/social-mcp/internal/mcptoken"
	"github.com/nhatnam1507/social-mcp/internal/oauth"
	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/provider"
	"github.com/nhatnam1507/social-mcp/internal/refresher"
	"github.com/nhatnam1507/social-mcp/internal/runner"
	"github.com/nhatnam1507/social-mcp/internal/secrets"
	"github.com/nhatnam1507/social-mcp/internal/service"
	"github.com/nhatnam1507/social-mcp/internal/store"
	"github.com/nhatnam1507/social-mcp/internal/tokens"
)

var (
	// ErrAlreadyRunning means another daemon holds the lock for this home.
	ErrAlreadyRunning = errors.New("daemon is already running")
	errLocked         = errors.New("lock held by another process")
)

// Options configures Run. Zero values use the real environment.
type Options struct {
	Home       string
	Version    string
	SocketPath string            // default paths.SocketPath(Home)
	Ephemeral  bool              // bind a random loopback port (tests)
	Keyring    secrets.Keyring   // default OS keyring
	Registry   provider.Registry // default: built from config
	Log        *slog.Logger
	Ready      func(Info) // called once the daemon is serving
}

// Info describes a running daemon.
type Info struct {
	PID        int
	ListenPort int
	Socket     string
	KeyBackend string
}

// Run runs the daemon until ctx is cancelled or a shutdown request arrives.
func Run(ctx context.Context, o Options) error {
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	warn, err := paths.Ensure(o.Home)
	if err != nil {
		return err
	}
	if warn != "" {
		log.Warn(warn)
	}

	// 1. Process lock: exactly one daemon per home.
	lock, err := tryLock(paths.Lock(o.Home))
	if errors.Is(err, errLocked) {
		return ErrAlreadyRunning
	}
	if err != nil {
		return fmt.Errorf("lock %s: %w", paths.Lock(o.Home), err)
	}
	defer lock.unlock()

	cfg, err := config.Load(paths.Config(o.Home))
	if err != nil {
		return err
	}

	// 2. Bind every listener before touching jobs, so a failed start changes nothing.
	sock := o.SocketPath
	if sock == "" {
		sock = paths.SocketPath(o.Home)
	}
	if err := os.Remove(sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket %s: %w", sock, err)
	}
	ctrlLn, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("listen on control socket %s: %w", sock, err)
	}
	defer ctrlLn.Close()
	if runtime.GOOS != "windows" {
		if err := os.Chmod(sock, 0o600); err != nil {
			return err
		}
	}
	loopLns, port, err := listenLoopback(cfg.Server.ListenPort, o.Ephemeral, log)
	if err != nil {
		return err
	}
	for _, ln := range loopLns {
		defer ln.Close()
	}

	// 3. MCP token, pidfile, storage and key.
	token, err := mcptoken.LoadOrCreate(paths.MCPToken(o.Home))
	if err != nil {
		return err
	}
	if err := writePID(paths.PID(o.Home)); err != nil {
		return err
	}
	defer os.Remove(paths.PID(o.Home))
	st, err := store.Open(paths.DB(o.Home))
	if err != nil {
		return err
	}
	defer st.Close()
	kr := o.Keyring
	if kr == nil {
		kr = secrets.KeyringFromEnv()
	}
	key, err := secrets.LoadKey(o.Home, kr, st)
	if err != nil {
		return err
	}
	sealer, err := secrets.NewSealer(key.Bytes)
	if err != nil {
		return err
	}
	reg := o.Registry
	if reg == nil {
		reg = buildProviders(cfg, log)
	}

	// 4. Engine.
	tm := tokens.NewManager(st, sealer, reg, log)
	run := runner.New(runner.Options{
		Store: st, Tokens: tm, Registry: reg, AllowedDirs: cfg.Uploads.AllowedDirs, Workers: cfg.Uploads.MaxConcurrent,
		Serialized: []string{"tiktok", "instagram"}, Log: log,
	})
	svc := service.NewLocal(service.Options{
		Store: st, Tokens: tm, Registry: reg, Sessions: oauth.NewSessions(), Jobs: run,
		AllowedDirs: cfg.Uploads.AllowedDirs, ListenPort: port, Version: o.Version, KeyBackend: key.Backend, StartedAt: time.Now(),
	})
	if err := run.Start(); err != nil {
		return err
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	refreshCtx, stopRefresh := context.WithCancel(context.Background())
	var refreshDone sync.WaitGroup
	refreshDone.Add(1)
	go func() {
		defer refreshDone.Done()
		(&refresher.Refresher{Store: st, Tokens: tm, Log: log}).Run(refreshCtx)
	}()

	// 5. Serve.
	ctrlSrv := &http.Server{Handler: control.NewHandler(svc, stop), ReadHeaderTimeout: 10 * time.Second}
	mux := http.NewServeMux()
	mux.Handle("GET /oauth/callback", callbackHandler(svc))
	mountMCP(mux, svc, port, token, o.Version)
	loopSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1+len(loopLns))
	go func() { serveErr <- ctrlSrv.Serve(ctrlLn) }()
	for _, ln := range loopLns {
		go func(ln net.Listener) { serveErr <- loopSrv.Serve(ln) }(ln)
	}
	log.Info("daemon started", "pid", os.Getpid(), "mcp", fmt.Sprintf("http://127.0.0.1:%d/mcp", port), "key_backend", key.Backend, "platforms", reg.Platforms())
	if o.Ready != nil {
		o.Ready(Info{PID: os.Getpid(), ListenPort: port, Socket: sock, KeyBackend: key.Backend})
	}

	var runErr error
	select {
	case <-runCtx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	}

	// 6. Shut down: stop intake, interrupt uploads (left resumable), stop refresher, close store.
	log.Info("daemon stopping")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctrlSrv.Shutdown(shutCtx)
	loopSrv.Shutdown(shutCtx)
	runStopCtx, cancelRun := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelRun()
	run.Stop(runStopCtx)
	stopRefresh()
	refreshDone.Wait()
	os.Remove(sock)
	return runErr
}

// listenLoopback binds 127.0.0.1:port and, when available, [::1]:port.
func listenLoopback(port int, ephemeral bool, log *slog.Logger) ([]net.Listener, int, error) {
	if ephemeral {
		port = 0
	}
	ln4, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, 0, fmt.Errorf("listen on 127.0.0.1:%d: %w (another process owns the port; find it with `lsof -i :%d` or `netstat -ano`, or change server.listen_port)", port, err, port)
	}
	actual := ln4.Addr().(*net.TCPAddr).Port
	lns := []net.Listener{ln4}
	if ephemeral {
		return lns, actual, nil
	}
	ln6, err := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(actual)))
	switch {
	case err == nil:
		lns = append(lns, ln6)
	case isAddrInUse(err):
		ln4.Close()
		return nil, 0, fmt.Errorf("listen on [::1]:%d: %w (another process owns the IPv6 loopback port, which could intercept logins)", actual, err)
	default:
		log.Info("IPv6 loopback unavailable; listening on 127.0.0.1 only", "err", err)
	}
	return lns, actual, nil
}

func isAddrInUse(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "address already in use") || strings.Contains(s, "only one usage of each socket address")
}
