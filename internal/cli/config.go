package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/config"
	"github.com/nhatnam1507/social-mcp/internal/daemon"
	"github.com/nhatnam1507/social-mcp/internal/mcptoken"
	"github.com/nhatnam1507/social-mcp/internal/paths"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Create and inspect configuration"}
	cmd.AddCommand(configInitCmd(), configPathCmd(), configMCPCmd())
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
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\nAdd your platform app credentials (see docs/platform-setup.md), then run `social-mcp daemon start`.\n", p)
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

func configMCPCmd() *cobra.Command {
	var rotate bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Print the MCP endpoint, bearer token and client setup snippets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			url := fmt.Sprintf("http://127.0.0.1:%d/mcp", cfg.Server.ListenPort)
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "MCP endpoint: %s\nBearer token: %s\n\n", url, tok)
			fmt.Fprintf(out, "Claude Code (HTTP):\n  claude mcp add --transport http social-mcp %s --header \"Authorization: Bearer %s\"\n\n", url, tok)
			fmt.Fprintf(out, "Clients that launch stdio servers (JSON config):\n  {\"mcpServers\": {\"social-mcp\": {\"command\": %q, \"args\": [\"mcp\"]}}}\n", exe)
			if rotate && daemon.Running(h) {
				fmt.Fprintln(out, "\nThe running daemon still uses the old token: restart it with `social-mcp daemon stop && social-mcp daemon start`.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&rotate, "rotate", false, "generate a new token (restart the daemon afterwards)")
	return cmd
}
