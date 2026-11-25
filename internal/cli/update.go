package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/daemon"
	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/update"
)

// releaseSource finds and downloads releases; replaced in tests.
type releaseSource interface {
	Latest(ctx context.Context) (string, error)
	Download(ctx context.Context, tag, dir string) (string, error)
}

// Seams for the update command; replaced in tests.
var (
	newReleaseSource = func(version string) releaseSource { return update.NewClient(version) }
	installedPath    = update.InstalledPath
	replaceBinary    = update.Replace
	stopDaemon       = func(home string) (bool, error) { return daemon.Stop(home, 30*time.Second) }
	// startDaemonWith starts the daemon through the new binary: after the swap, os.Executable() of this
	// process can point at the replaced file (on Linux it reads "<path> (deleted)").
	startDaemonWith = func(exe, home string, out io.Writer) error {
		cmd := exec.Command(exe, "daemon", "start")
		cmd.Env = append(os.Environ(), paths.EnvHome+"="+home)
		cmd.Stdout, cmd.Stderr = out, out
		return cmd.Run()
	}
)

func updateCmd(version string) *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Install the latest release (or --version) in place and restart a running daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd.Context(), cmd.OutOrStdout(), version, target)
		},
	}
	cmd.Flags().StringVar(&target, "version", "", "release tag to install, for example v0.2.0 (default: the latest stable release)")
	return cmd
}

func runUpdate(ctx context.Context, out io.Writer, current, target string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	src := newReleaseSource(current)
	if target == "" {
		latest, err := src.Latest(ctx)
		if errors.Is(err, update.ErrNoRelease) {
			fmt.Fprintln(out, "no stable release has been published yet; nothing to update")
			return nil
		}
		if err != nil {
			return err
		}
		target = latest
	} else if !update.IsRelease(target) {
		return fmt.Errorf("--version must be a release tag such as v0.2.0, got %q", target)
	}
	if !update.IsRelease(current) {
		fmt.Fprintf(out, "current version %s is a development build\n", current)
	} else if current == target {
		fmt.Fprintf(out, "social-mcp %s is already up to date\n", current)
		return nil
	}

	exe, err := installedPath()
	if err != nil {
		return fmt.Errorf("find the installed binary: %w", err)
	}
	update.CleanupOld(exe)
	fmt.Fprintf(out, "downloading social-mcp %s\n", target)
	tmp, err := src.Download(ctx, target, filepath.Dir(exe))
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w\n%s is not writable; re-run the installer or fix the folder's permissions", err, filepath.Dir(exe))
	}
	if err != nil {
		return err
	}

	// Stop the daemon only once the new binary is downloaded and verified.
	h, err := home()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	wasRunning := daemonRunning(h)
	if wasRunning {
		fmt.Fprintln(out, "stopping the daemon (running uploads resume after the restart)")
		if _, err := stopDaemon(h); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("stop the daemon: %w; nothing was installed", err)
		}
	}
	if err := replaceBinary(tmp, exe); err != nil {
		os.Remove(tmp)
		err = fmt.Errorf("install %s: %w", exe, err)
		if wasRunning {
			if serr := startDaemonWith(exe, h, out); serr != nil {
				err = fmt.Errorf("%w; restarting the daemon also failed: %v", err, serr)
			}
		}
		return err
	}
	fmt.Fprintf(out, "updated social-mcp %s -> %s (%s)\n", current, target, exe)
	if wasRunning {
		if err := startDaemonWith(exe, h, out); err != nil {
			return fmt.Errorf("social-mcp was updated, but the daemon did not start: %w; run `social-mcp daemon start`", err)
		}
	}
	return nil
}
