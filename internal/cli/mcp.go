package cli

import (
	"github.com/spf13/cobra"
)

func mcpCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve MCP over stdio, backed by the running daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return nil
		},
	}
}
