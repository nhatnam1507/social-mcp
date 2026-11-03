package daemon

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nhatnam1507/social-mcp/internal/mcpserver"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

// mountMCP serves the MCP endpoint on the loopback mux. Requests are stateless: every tool
// is request/response, so no session state accumulates in the long-running daemon.
func mountMCP(mux *http.ServeMux, svc service.Service, port int, token, version string) {
	server := mcpserver.New(svc, version)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	mux.Handle("/mcp", mcpserver.Protect(h, port, token))
}
