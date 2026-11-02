package cli

import (
	"context"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/control"
	"github.com/nhatnam1507/social-mcp/internal/mcpserver"
	"github.com/nhatnam1507/social-mcp/internal/paths"
)

func mcpCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve MCP over stdio, backed by the running daemon",
		Long:  "For MCP clients that launch servers as subprocesses. Requires `social-mcp daemon start`.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			client := control.NewClient(paths.SocketPath(h))
			if _, err := client.Status(); err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			return mcpserver.New(client, version).Run(ctx, &mcp.StdioTransport{})
		},
	}
}
