package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeLatest struct {
	tag   string
	err   error
	calls int
}

func (f *fakeLatest) Latest(context.Context) (string, error) { f.calls++; return f.tag, f.err }

func newChecker(t *testing.T, version string, src *fakeLatest, now *time.Time) *Checker {
	t.Helper()
	return &Checker{
		Path:    filepath.Join(t.TempDir(), "update-check.json"),
		Version: version,
		Source:  src,
		Now:     func() time.Time { return *now },
	}
}

func TestCheckNowAtMostOncePerInterval(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	src := &fakeLatest{tag: "v0.2.0"}
	c := newChecker(t, "v0.1.0", src, &now)
	c.CheckNow(context.Background())
	now = now.Add(23 * time.Hour)
	c.CheckNow(context.Background())
	if src.calls != 1 {
		t.Fatalf("looked up %d times within 24h, want 1", src.calls)
	}
	now = now.Add(2 * time.Hour)
	c.CheckNow(context.Background())
	if src.calls != 2 {
		t.Fatalf("looked up %d times after 25h, want 2", src.calls)
	}
	st, err := LoadState(c.Path)
	if err != nil || st.Latest != "v0.2.0" || !st.CheckedAt.Equal(now) {
		t.Fatalf("state %+v %v", st, err)
	}
}

func TestCheckNowSurvivesRestartsAndFailures(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	src := &fakeLatest{err: errors.New("network down")}
	c := newChecker(t, "v0.1.0", src, &now)
	c.CheckNow(context.Background())
	if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a failed lookup must not record a check, so the next wake retries")
	}
	src.err = ErrNoRelease
	c.CheckNow(context.Background())
	st, _ := LoadState(c.Path)
	if st.CheckedAt.IsZero() || st.Latest != "" {
		t.Fatalf("no-release state %+v", st)
	}
	// A new daemon (restart) reads the record and does not look up again.
	restarted := &Checker{Path: c.Path, Version: "v0.1.0", Source: src, Now: c.Now}
	restarted.CheckNow(context.Background())
	if src.calls != 2 {
		t.Fatalf("calls = %d, want the restarted daemon to reuse the recent check", src.calls)
	}
}

func TestTakeNoticeOncePerRelease(t *testing.T) {
	now := time.Now()
	src := &fakeLatest{tag: "v0.2.0"}
	c := newChecker(t, "v0.1.0", src, &now)
	if n := c.TakeNotice(); n != "" {
		t.Fatalf("notice before any check: %q", n)
	}
	c.CheckNow(context.Background())
	if n := c.TakeNotice(); !strings.Contains(n, "v0.2.0 is available (you have v0.1.0)") {
		t.Fatalf("first notice = %q", n)
	}
	if n := c.TakeNotice(); n != "" {
		t.Fatalf("second notice = %q, want none", n)
	}
	src.tag = "v0.3.0"
	now = now.Add(25 * time.Hour)
	c.CheckNow(context.Background())
	if n := c.TakeNotice(); !strings.Contains(n, "v0.3.0") {
		t.Fatalf("notice for the next release = %q", n)
	}
}

func TestTakeNoticeQuietWhenCurrent(t *testing.T) {
	now := time.Now()
	c := newChecker(t, "v0.2.0", &fakeLatest{tag: "v0.2.0"}, &now)
	c.CheckNow(context.Background())
	if n := c.TakeNotice(); n != "" {
		t.Fatalf("notice on the latest version: %q", n)
	}
}

func TestCheckEnabled(t *testing.T) {
	if !CheckEnabled(true, "v0.1.0") || CheckEnabled(false, "v0.1.0") || CheckEnabled(true, "dev") {
		t.Fatal("CheckEnabled ignores config or build type")
	}
	t.Setenv(EnvNoCheck, "1")
	if CheckEnabled(true, "v0.1.0") {
		t.Fatal("CheckEnabled ignores " + EnvNoCheck)
	}
}

func TestRunStopsWithContext(t *testing.T) {
	now := time.Now()
	src := &fakeLatest{tag: "v0.2.0"}
	c := newChecker(t, "v0.1.0", src, &now)
	c.FirstDelay, c.Wake = time.Millisecond, time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if st, _ := LoadState(c.Path); st.Latest == "v0.2.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run never checked")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
