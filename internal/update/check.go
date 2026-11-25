package update

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EnvNoCheck turns off the daily update check when set to any non-empty value.
const EnvNoCheck = "SOCIAL_MCP_NO_UPDATE_CHECK"

// CheckEnabled reports whether the update check and its notices apply: the config allows it, the
// environment does not turn it off, and this is a release build (development builds never check).
func CheckEnabled(configCheck bool, version string) bool {
	return configCheck && os.Getenv(EnvNoCheck) == "" && IsRelease(version)
}

// State is what the daemon records about the latest release. Only the daemon writes it.
type State struct {
	CheckedAt time.Time `json:"checked_at"`
	// Latest is the newest stable release tag, or empty when none exists.
	Latest string `json:"latest,omitempty"`
	// AgentNotified is the release the agent was last told about.
	AgentNotified string `json:"agent_notified,omitempty"`
}

// LoadState reads the state file; a missing file is an empty state.
func LoadState(path string) (State, error) {
	var st State
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}

// SaveState writes the state file atomically.
func SaveState(path string, st State) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(b, '\n'))
}

// WriteFileAtomic replaces path with data through a temporary file in the same folder, so a reader never
// sees a partial file. The temporary name starts with the target's name, keeping it recognizable as a
// social-mcp file.
func WriteFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

// latestSource finds the newest stable release.
type latestSource interface {
	Latest(ctx context.Context) (string, error)
}

// Checker looks up the latest release at most once per Interval and hands the agent its notice once per
// new release. It runs inside the daemon, so CLI commands never wait on the network.
type Checker struct {
	Path    string // state file
	Version string // the daemon's own version
	Source  latestSource
	Log     *slog.Logger
	Now     func() time.Time

	// Interval is the minimum time between lookups (default 24h). Wake is how often Run reconsiders
	// (default 1h) and FirstDelay the wait after startup (default 1m), so a crash loop cannot hammer
	// the release host.
	Interval, Wake, FirstDelay time.Duration

	mu sync.Mutex // serializes read-modify-write of the state file
}

func (c *Checker) defaults() {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	if c.Interval == 0 {
		c.Interval = 24 * time.Hour
	}
	if c.Wake == 0 {
		c.Wake = time.Hour
	}
	if c.FirstDelay == 0 {
		c.FirstDelay = time.Minute
	}
}

// Run checks after FirstDelay, then reconsiders every Wake until ctx is done.
func (c *Checker) Run(ctx context.Context) {
	c.defaults()
	wait := c.FirstDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		c.CheckNow(ctx)
		wait = c.Wake
	}
}

// CheckNow looks up the latest release unless the last lookup is younger than Interval. Failures are
// logged and retried on a later wake; they never affect the daemon's other work.
func (c *Checker) CheckNow(ctx context.Context) {
	c.defaults()
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := LoadState(c.Path)
	if err != nil {
		c.Log.Debug("update check: unreadable state, starting over", "err", err)
		st = State{}
	}
	now := c.Now()
	if !st.CheckedAt.IsZero() && now.Sub(st.CheckedAt) < c.Interval && !st.CheckedAt.After(now) {
		return
	}
	latest, err := c.Source.Latest(ctx)
	switch {
	case errors.Is(err, ErrNoRelease):
		latest = ""
	case err != nil:
		c.Log.Debug("update check failed", "err", err)
		return
	}
	st.CheckedAt, st.Latest = now, latest
	if err := SaveState(c.Path, st); err != nil {
		c.Log.Debug("update check: could not save state", "err", err)
		return
	}
	if Newer(c.Version, latest) {
		c.Log.Info("a new social-mcp release is available", "latest", latest, "current", c.Version)
	}
}

// TakeNotice returns the update notice for the agent once per new release, and "" otherwise.
func (c *Checker) TakeNotice() string {
	c.defaults()
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := LoadState(c.Path)
	if err != nil || !Newer(c.Version, st.Latest) || st.AgentNotified == st.Latest {
		return ""
	}
	st.AgentNotified = st.Latest
	if err := SaveState(c.Path, st); err != nil {
		// Without a record the agent would hear it again on every call; stay quiet instead.
		c.Log.Debug("update notice: could not save state", "err", err)
		return ""
	}
	return Notice(c.Version, st.Latest)
}
