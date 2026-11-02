// Package mcpserver exposes the service layer as MCP tools.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nhatnam1507/social-mcp/internal/service"
)

const instructions = `social-mcp uploads local video files to the user's connected YouTube, TikTok, Facebook Page and Instagram accounts.

Workflow:
1. When the user gives a video file path, call list_accounts and pick the accounts the user asked for. If it is unclear which accounts or platforms they mean, ask the user.
2. Call upload_video with the exact absolute file path, the chosen account_ids, a title, and optionally description, tags and privacy.
3. Uploads run in the background. Poll get_upload_status for each job until its status is succeeded, failed, cancelled or needs_review, then report the result and URL to the user.

Rules:
- Privacy defaults to "private". Use "public" only when the user asked for a public post.
- Respect each account's capabilities.restrictions:
  - draft_only: the video lands in the user's TikTok inbox as a draft; the user adds the caption, chooses privacy and posts in the TikTok app.
  - no_private_option: Instagram Reels are always public; only upload there when the user wants a public post.
  - visible_to_app_roles_only: the Meta app is in development mode, so the post is not visible to the public yet.
- Never claim a post is public unless the job outcome is "published" and its privacy is "public".
- needs_review means the platform may or may not have published the video; tell the user to check the platform before uploading again.
- Accounts with health "needs_reauth" cannot upload; tell the user to run the reauth command shown in health_detail. Agents cannot connect or remove accounts.`

// New returns an MCP server backed by svc.
func New(svc service.Service, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "social-mcp", Title: "Social video uploader", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	addTools(s, svc)
	return s
}
