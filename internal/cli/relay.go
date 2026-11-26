package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/control"
	"github.com/nhatnam1507/social-mcp/internal/daemon"
	"github.com/nhatnam1507/social-mcp/internal/mcpserver"
	"github.com/nhatnam1507/social-mcp/internal/paths"
)

func relayCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "relay",
		Short: "Relay between an AI app and the daemon (the app runs this; you never need to)",
		Long: "The command AI apps start to talk to social-mcp: it relays MCP messages over stdin/stdout to the\n" +
			"running daemon, and starts the daemon when it is not running. `social-mcp connect` sets apps up to use it.",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			h, err := home()
			if err != nil {
				return err
			}
			client := control.NewClient(paths.SocketPath(h)).AutoStart(ensureDaemon(h))
			if _, err := client.Status(); err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			return mcpserver.New(client, version).Run(ctx, &mcp.StdioTransport{})
		},
	}
}

// ensureDaemon returns a function that starts the daemon in the background. Concurrent tool calls
// share one start attempt, and a daemon that came up in the meantime counts as success.
func ensureDaemon(home string) func() error {
	var mu sync.Mutex
	return func() error {
		mu.Lock()
		defer mu.Unlock()
		_, err := daemon.Start(home, 10*time.Second)
		if errors.Is(err, daemon.ErrAlreadyRunning) {
			return nil
		}
		return err
	}
}
