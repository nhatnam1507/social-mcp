package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/config"
	"github.com/nhatnam1507/social-mcp/internal/daemon"
	"github.com/nhatnam1507/social-mcp/internal/mcptoken"
	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/wsl"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Create and inspect configuration",
		// Reject unknown subcommands, such as `config mcp` from older releases, instead of printing help.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(configInitCmd(), configPathCmd())
	return cmd
}

func configInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config.toml",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			if _, err := paths.Ensure(h); err != nil {
				return err
			}
			p := paths.Config(h)
			if _, err := os.Stat(p); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", p)
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := os.WriteFile(p, []byte(config.Template()), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\nRun `social-mcp daemon start`, then `social-mcp account add <platform>`.\n", p)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config.toml")
	return cmd
}

func configPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the home directory and config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "home:   %s\nconfig: %s\n", h, paths.Config(h))
			return nil
		},
	}
}

// showSetup prints how to connect any AI app, including the HTTP endpoint and its token, which it
// creates (or, with rotate, replaces) on first use.
func showSetup(cmd *cobra.Command, rotate bool) error {
	h, err := home()
	if err != nil {
		return err
	}
	if _, err := paths.Ensure(h); err != nil {
		return err
	}
	cfg, err := config.Load(paths.Config(h))
	if err != nil {
		return err
	}
	var tok string
	if rotate {
		tok, err = mcptoken.Rotate(paths.MCPToken(h))
	} else {
		tok, err = mcptoken.LoadOrCreate(paths.MCPToken(h))
	}
	if err != nil {
		return err
	}
	exe, _ := os.Executable()
	port, err := config.ListenPort(os.Getenv)
	if err != nil {
		return err
	}
	if w := cfg.RetiredWarning(); w != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: "+w)
	}
	printSetup(cmd.OutOrStdout(), exe, fmt.Sprintf("http://127.0.0.1:%d/mcp", port), tok)
	if rotate && daemon.Running(h) {
		fmt.Fprintln(cmd.OutOrStdout(), "\nThe running daemon still uses the old token: restart it with `social-mcp daemon stop && social-mcp daemon start`.")
	}
	return nil
}

// printSetup prints how to connect an AI app, for a person or for an agent asked to do it for them.
func printSetup(out io.Writer, exe, url, tok string) {
	entry := func(s stdioServer) string {
		b, _ := json.Marshal(map[string]map[string]stdioServer{"mcpServers": {serverName: s}})
		return string(b)
	}
	fmt.Fprint(out, "Connect social-mcp to your AI app once. The app then starts social-mcp by itself (and the daemon,\n"+
		"when it is not running), so you never need to do this again.\n\n")
	fmt.Fprint(out, "Easiest: let social-mcp do it\n"+
		"  social-mcp connect claude-desktop\n"+
		"  social-mcp connect claude-code\n"+
		"  social-mcp connect cursor\n"+
		"  social-mcp connect codex\n"+
		"  social-mcp connect antigravity\n\n")
	fmt.Fprintf(out, "Any other app on this machine: add this to its MCP settings file:\n  %s\n\n", entry(localServer(exe)))
	if wsl.Detected() {
		fmt.Fprintf(out, "Apps on Windows (social-mcp runs in WSL): use this entry instead; wsl.exe starts social-mcp inside WSL:\n  %s\n\n", entry(windowsServer(exe)))
	}
	fmt.Fprint(out, "Already connected through the HTTP endpoint or mcp-remote? Replace that social-mcp entry with one above.\n\n")
	fmt.Fprintf(out, "HTTP, only for apps that cannot start a command (the daemon must already be running when the app starts):\n"+
		"  MCP endpoint: %s\n  Bearer token: %s\n"+
		"  claude mcp add --transport http %s %s --header \"Authorization: Bearer %s\"\n"+
		"  (replace the token with `social-mcp connect --rotate-token`)\n", url, tok, serverName, url, tok)
}
