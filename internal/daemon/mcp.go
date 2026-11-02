package daemon

import (
	"net/http"

	"github.com/nhatnam1507/social-mcp/internal/service"
)

// mountMCP serves the MCP endpoint on the loopback mux.
func mountMCP(mux *http.ServeMux, svc service.Service, port int, token, version string) {
}
