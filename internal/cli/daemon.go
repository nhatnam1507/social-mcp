package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/control"
	"github.com/nhatnam1507/social-mcp/internal/daemon"
	"github.com/nhatnam1507/social-mcp/internal/logx"
	"github.com/nhatnam1507/social-mcp/internal/paths"
)

func daemonCmd(version string) *cobra.Command {
	cmd := &cobra.Command{Use: "daemon", Short: "Manage the background daemon"}
	cmd.AddCommand(daemonStartCmd(), daemonStopCmd(), daemonStatusCmd(), daemonLogsCmd(), daemonRunCmd(version))
	return cmd
}

func daemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the background",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			st, err := daemon.Start(h, 10*time.Second)
			out := cmd.OutOrStdout()
			if errors.Is(err, daemon.ErrAlreadyRunning) {
				fmt.Fprintf(out, "daemon is already running (pid %d)\n", st.PID)
				return nil
			}
			if errors.Is(err, daemon.ErrUnresponsive) {
				return &ExitError{Code: 4, Err: err}
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "daemon started (pid %d)\nMCP endpoint: %s\nkey storage:  %s\nlogs:         %s\n", st.PID, st.ListenURL, st.KeyBackend, paths.Log(h))
			return nil
		},
	}
}

func daemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon; running uploads resume on the next start",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			forced, err := daemon.Stop(h, 30*time.Second)
			if errors.Is(err, control.ErrDaemonNotRunning) {
				fmt.Fprintln(cmd.OutOrStdout(), "daemon is not running")
				return nil
			}
			if err != nil {
				return err
			}
			if forced {
				fmt.Fprintln(cmd.OutOrStdout(), "daemon did not respond to the shutdown request and was terminated")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "daemon stopped")
			}
			return nil
		},
	}
}

func daemonStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status (exit code 3 when stopped)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			st, err := probeStatus(h)
			out := cmd.OutOrStdout()
			if err != nil && daemonRunning(h) {
				// The process lock is held: the daemon runs even though its socket does not answer.
				fmt.Fprintln(out, "running (process lock held), but the control socket does not answer; run `social-mcp daemon stop`")
				return &ExitError{Code: 4}
			}
			if errors.Is(err, control.ErrDaemonNotRunning) {
				fmt.Fprintln(out, "stopped")
				return &ExitError{Code: 3}
			}
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(out, st)
			}
			fmt.Fprintf(out, "running (pid %d, version %s, up %s)\n", st.PID, st.Version, time.Since(st.StartedAt).Round(time.Second))
			fmt.Fprintf(out, "MCP endpoint:   %s\nkey storage:    %s\nplatforms:      %s\nactive uploads: %d\n",
				st.ListenURL, st.KeyBackend, strings.Join(st.Platforms, ", "), st.ActiveUploads)
			platforms := make([]string, 0, len(st.Accounts))
			for p := range st.Accounts {
				platforms = append(platforms, p)
			}
			sort.Strings(platforms)
			for _, p := range platforms {
				var parts []string
				for health, n := range st.Accounts[p] {
					parts = append(parts, fmt.Sprintf("%d %s", n, health))
				}
				sort.Strings(parts)
				fmt.Fprintf(out, "accounts %-10s %s\n", p+":", strings.Join(parts, ", "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func daemonLogsCmd() *cobra.Command {
	var lines int
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the daemon log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			if lines < 0 {
				return fmt.Errorf("--lines must not be negative")
			}
			return tailFile(cmd.Context(), cmd.OutOrStdout(), paths.Log(h), lines, follow)
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "number of lines to show")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new lines")
	return cmd
}

func daemonRunCmd(version string) *cobra.Command {
	var detached bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon in the foreground (for service managers)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				// After the first signal, restore default handling so a second Ctrl+C ends a
				// stuck shutdown. (`daemon stop` escalates to SIGKILL on its own.)
				<-ctx.Done()
				stop()
			}()
			log := slog.New(logx.NewHandler(slog.NewTextHandler(cmd.ErrOrStderr(), nil)))
			err = daemon.Run(ctx, daemon.Options{Home: h, Version: version, Log: log})
			if err != nil {
				log.Error("daemon failed", "err", err)
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&detached, "detached", false, "started by `daemon start`")
	cmd.Flags().MarkHidden("detached")
	return cmd
}

func tailFile(ctx context.Context, w io.Writer, path string, lines int, follow bool) error {
	data, end, err := daemon.ReadTail(path, 4<<20)
	if errors.Is(err, os.ErrNotExist) && !follow {
		fmt.Fprintf(w, "no log yet at %s\n", path)
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	if len(data) > 0 {
		fmt.Fprintln(w, strings.Join(all, "\n"))
	}
	if !follow {
		return nil
	}
	offset := end
	if ctx == nil {
		ctx = context.Background()
	}
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	for {
		select {
		case <-sigCtx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		fi, err := f.Stat()
		if err == nil && fi.Size() < offset {
			offset = 0 // log was truncated
		}
		if err == nil && fi.Size() > offset {
			f.Seek(offset, io.SeekStart)
			n, _ := io.Copy(w, f)
			offset += n
		}
		f.Close()
	}
}
