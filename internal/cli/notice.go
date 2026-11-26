package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/config"
	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/update"
)

// noNoticeCommands never print the update notice: `relay` speaks a protocol on stdio, `daemon run` is for
// service managers, and `update` and `version` already deal with versions.
var noNoticeCommands = map[string]bool{
	"social-mcp relay":      true,
	"social-mcp daemon run": true,
	"social-mcp update":     true,
	"social-mcp version":    true,
}

// printUpdateNotice tells the user once per release, on stderr, that the daemon found a newer release.
// It never touches the network and stays silent on any error.
func printUpdateNotice(cmd *cobra.Command, version string, stderr io.Writer) {
	if noNoticeCommands[cmd.CommandPath()] {
		return
	}
	h, err := resolveHome()
	if err != nil {
		return
	}
	cfg, err := config.Load(paths.Config(h))
	if err != nil || !update.CheckEnabled(cfg.Updates.Check, version) {
		return
	}
	st, err := update.LoadState(paths.UpdateCheck(h))
	if err != nil || !update.Newer(version, st.Latest) {
		return
	}
	seen, _ := os.ReadFile(paths.UpdateNoticeCLI(h))
	if strings.TrimSpace(string(seen)) == st.Latest {
		return
	}
	if err := update.WriteFileAtomic(paths.UpdateNoticeCLI(h), []byte(st.Latest+"\n")); err != nil {
		return // without a record the notice would repeat on every command
	}
	fmt.Fprintln(stderr, update.Notice(version, st.Latest))
}
