// Package cli implements the social-mcp command line.
package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/control"
	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

// ExitError carries a process exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// dialService connects to the daemon; replaced in tests.
var dialService = func(home string) service.Service {
	return control.NewClient(paths.SocketPath(home))
}

// resolveHome finds the home directory; replaced in tests.
var resolveHome = paths.ResolveHome

// NewRoot builds the command tree.
func NewRoot(version string, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "social-mcp",
		Short:         "Upload videos to YouTube, TikTok, Facebook Pages and Instagram from AI agents over MCP",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(daemonCmd(version), accountCmd(), configCmd(), mcpCmd(version), versionCmd(version))
	return root
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func home() (string, error) {
	h, err := resolveHome()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return h, nil
}
