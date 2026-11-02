// Command social-mcp uploads videos to social platforms on behalf of AI agents over MCP.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/nhatnam1507/social-mcp/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.NewRoot(version, os.Stdout, os.Stderr).Execute(); err != nil {
		code := 1
		var ee *cli.ExitError
		if errors.As(err, &ee) {
			code = ee.Code
		}
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, "error:", msg)
		}
		os.Exit(code)
	}
}
